package chain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// transferTopic is keccak256("Transfer(address,address,uint256)"), the
// first topic of every ERC-20 transfer log: a constant of the standard,
// kept as a literal rather than computed with a keccak dependency.
const transferTopic = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

const (
	// timeout bounds one call to a node.
	timeout = 15 * time.Second
	// maxBody caps what is read of an answer: a page of logs or a
	// transaction is well under it.
	maxBody = 4 << 20
	// span is the most EVM blocks one eth_getLogs asks about, and spans how
	// many one read takes, so a cursor far behind catches up over a few
	// reads instead of one request a public node refuses.
	span  = 1000
	spans = 5
	// pages is the most pages of TRON transfers one read follows.
	pages = 5
	// solanaListPages is the most pages of Solana signatures one read lists,
	// and solanaReads the most transactions it fetches. Past either it
	// stops and saves how far it got (see Solana), so a flood of junk to
	// the wallet slows reading down and never stops it.
	solanaListPages = 10
	solanaReads     = 50
)

// Transfer is one payment into a watched wallet: the transaction, its
// place in it, the amount in the rail's smallest unit and who sent it as
// the chain names them.
type Transfer struct {
	Tx     string
	Idx    int
	Amount *big.Int
	From   string
	// At is when the chain took it, where the reader knows (Solana); zero
	// otherwise.
	At time.Time
}

// Reader reads the chains. The zero value reads every rail's own public
// endpoint.
type Reader struct {
	HTTP *http.Client
	// RPC is an endpoint by chain ("base"), in place of the rail's own: a
	// paid node, or a test server.
	RPC map[string]string
	// TronKey is a TronGrid API key, sent if set: without one TronGrid
	// answers a few calls a second, which is plenty for one wallet.
	TronKey string
	// Prices is where USD prices are read; Coinbase's public spot price
	// when "".
	Prices string
}

func (r *Reader) endpoint(rail Rail) string {
	if u := r.RPC[rail.Chain]; u != "" {
		return u
	}
	return rail.RPC
}

func (r *Reader) client() *http.Client {
	if r.HTTP != nil {
		return r.HTTP
	}
	return &http.Client{Timeout: timeout}
}

// rpcError is a JSON-RPC error. Nodes send it with HTTP 200, so the status
// alone would read a refusal as an empty answer.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message) }

// rpc calls method on a JSON-RPC endpoint and decodes its result into out.
func (r *Reader) rpc(ctx context.Context, endpoint, method string, params []any, out any) error {
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		return err
	}
	raw, err := r.do(ctx, http.MethodPost, endpoint, body, nil)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	var env struct {
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	if env.Error != nil {
		return fmt.Errorf("%s: %w", method, env.Error)
	}
	if err := json.Unmarshal(env.Result, out); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	return nil
}

// do sends one request and returns the size capped body of a 2xx answer.
func (r *Reader) do(ctx context.Context, method, endpoint string, body []byte, header http.Header) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, rd)
	if err != nil {
		return nil, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := r.client().Do(req)
	if err != nil {
		// The URL can carry a key: say which host, not the request.
		return nil, fmt.Errorf("reaching %s: %w", host(endpoint), unwrapURL(err))
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return nil, err
	}
	if res.StatusCode/100 != 2 {
		return nil, fmt.Errorf("%s answered %d", host(endpoint), res.StatusCode)
	}
	return raw, nil
}

func host(endpoint string) string {
	if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
		return u.Host
	}
	return "the node"
}

func unwrapURL(err error) error {
	if ue, ok := err.(*url.Error); ok {
		return ue.Err
	}
	return err
}

// hexInt reads a 0x quantity.
func hexInt(s string) (*big.Int, error) {
	v, ok := new(big.Int).SetString(strings.TrimPrefix(s, "0x"), 16)
	if !ok || !strings.HasPrefix(s, "0x") {
		return nil, fmt.Errorf("%q is not a hex quantity", s)
	}
	return v, nil
}

// EVM lists the transfers of rail's token into to in the blocks from
// from on, as far as the chain is final enough to trust (its
// Confirmations below the head), and where the next read starts. A from of
// 0 starts at the current final block without reading anything before it,
// so a wallet's history is never taken for payments.
func (r *Reader) EVM(ctx context.Context, rail Rail, to string, from uint64) ([]Transfer, uint64, error) {
	if !evmAddress.MatchString(to) {
		return nil, from, fmt.Errorf("evm: %q is not an address", to)
	}
	endpoint := r.endpoint(rail)
	var head string
	if err := r.rpc(ctx, endpoint, "eth_blockNumber", []any{}, &head); err != nil {
		return nil, from, err
	}
	h, err := hexInt(head)
	if err != nil || !h.IsUint64() {
		return nil, from, fmt.Errorf("eth_blockNumber: %q", head)
	}
	if h.Uint64() < rail.Confirmations {
		return nil, from, nil
	}
	final := h.Uint64() - rail.Confirmations
	if from == 0 {
		return nil, final + 1, nil
	}
	topic := "0x" + strings.Repeat("0", 24) + strings.ToLower(strings.TrimPrefix(to, "0x"))
	var out []Transfer
	for range spans {
		if from > final {
			break
		}
		last := min(final, from+span-1)
		var logs []struct {
			Address  string   `json:"address"`
			Topics   []string `json:"topics"`
			Data     string   `json:"data"`
			Tx       string   `json:"transactionHash"`
			LogIndex string   `json:"logIndex"`
			Removed  bool     `json:"removed"`
		}
		err := r.rpc(ctx, endpoint, "eth_getLogs", []any{map[string]any{
			"fromBlock": "0x" + strconv.FormatUint(from, 16),
			"toBlock":   "0x" + strconv.FormatUint(last, 16),
			"address":   rail.Token,
			"topics":    []any{transferTopic, nil, topic},
		}}, &logs)
		if err != nil {
			return out, from, err
		}
		for _, l := range logs {
			// A node that ignored the filter, or a reorged log, is not a
			// payment.
			if l.Removed || !strings.EqualFold(l.Address, rail.Token) || len(l.Topics) != 3 || l.Topics[0] != transferTopic || len(l.Topics[1]) != len(topic) || !strings.EqualFold(l.Topics[2], topic) {
				continue
			}
			amount, err := hexInt(l.Data)
			if err != nil {
				return out, from, fmt.Errorf("eth_getLogs: %w", err)
			}
			idx, err := hexInt(l.LogIndex)
			if err != nil || !idx.IsInt64() {
				return out, from, fmt.Errorf("eth_getLogs: log index %q", l.LogIndex)
			}
			if amount.Sign() <= 0 {
				continue
			}
			out = append(out, Transfer{Tx: strings.ToLower(l.Tx), Idx: int(idx.Int64()), Amount: amount, From: "0x" + strings.ToLower(l.Topics[1][len(l.Topics[1])-40:])})
		}
		from = last + 1
	}
	return out, from, nil
}

// Tron lists the confirmed transfers of rail's token into to from since
// (Unix ms) on, and the block time the next read starts from. A transfer at
// exactly that time is read again next time, which the store takes as
// already seen. A since of 0 starts now without reading anything before.
func (r *Reader) Tron(ctx context.Context, rail Rail, to string, since int64, now time.Time) ([]Transfer, int64, error) {
	if _, err := tronHex(to); err != nil {
		return nil, since, err
	}
	if since == 0 {
		return nil, now.UnixMilli(), nil
	}
	q := url.Values{
		"only_confirmed":   {"true"},
		"only_to":          {"true"},
		"contract_address": {rail.Token},
		"min_timestamp":    {strconv.FormatInt(since, 10)},
		"order_by":         {"block_timestamp,asc"},
		"limit":            {"200"},
	}
	header := http.Header{}
	if r.TronKey != "" {
		header.Set("TRON-PRO-API-KEY", r.TronKey)
	}
	next := since
	var out []Transfer
	for range pages {
		raw, err := r.do(ctx, http.MethodGet, strings.TrimRight(r.endpoint(rail), "/")+"/v1/accounts/"+to+"/transactions/trc20?"+q.Encode(), nil, header)
		if err != nil {
			return out, since, fmt.Errorf("trongrid: %w", err)
		}
		var page struct {
			Success bool `json:"success"`
			Data    []struct {
				Tx    string `json:"transaction_id"`
				Token struct {
					Address string `json:"address"`
				} `json:"token_info"`
				Time  int64  `json:"block_timestamp"`
				From  string `json:"from"`
				To    string `json:"to"`
				Type  string `json:"type"`
				Value string `json:"value"`
			} `json:"data"`
			Meta struct {
				Fingerprint string `json:"fingerprint"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(raw, &page); err != nil || !page.Success {
			return out, since, fmt.Errorf("trongrid: an answer it didn't mean: %v", err)
		}
		for _, d := range page.Data {
			next = max(next, d.Time)
			if d.Type != "Transfer" || d.To != to || d.Token.Address != rail.Token {
				continue
			}
			amount, ok := new(big.Int).SetString(d.Value, 10)
			if !ok {
				return out, since, fmt.Errorf("trongrid: %q is not an amount", d.Value)
			}
			if amount.Sign() <= 0 {
				continue
			}
			// ponytail: TronGrid lists no log index, so two transfers to
			// this wallet in one transaction count as the first; a
			// transaction's events if one ever pays twice at once.
			out = append(out, Transfer{Tx: d.Tx, Amount: amount, From: d.From})
		}
		if page.Meta.Fingerprint == "" || len(page.Data) == 0 {
			break
		}
		q.Set("fingerprint", page.Meta.Fingerprint)
	}
	return out, next, nil
}

// Accounts is where rail's payments into owner land on Solana: the token
// accounts owner holds for its mint (a wallet derives the associated one
// from the owner, but an owner can hold more), or owner itself for SOL.
// None yet is fine: the first payment opens the account, and it is found
// on the next read.
func (r *Reader) Accounts(ctx context.Context, rail Rail, owner string) ([]string, error) {
	if err := solanaKey(owner); err != nil {
		return nil, err
	}
	if rail.Token == "" {
		return []string{owner}, nil
	}
	var res struct {
		Value []struct {
			Pubkey string `json:"pubkey"`
		} `json:"value"`
	}
	if err := r.rpc(ctx, r.endpoint(rail), "getTokenAccountsByOwner", []any{owner, map[string]string{"mint": rail.Token}, map[string]string{"encoding": "jsonParsed", "commitment": "finalized"}}, &res); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(res.Value))
	for _, v := range res.Value {
		out = append(out, v.Pubkey)
	}
	slices.Sort(out)
	return out, nil
}

// Solana lists what arrived for owner on rail in account's finalized
// transactions after the cursor, oldest first, and the cursor the next read
// resumes from. A cursor is the last signature read through, or that and
// "|" and a frontier: the oldest signature a listing too long for one read
// reached, which the next read lists back from instead of from the newest.
// So a backlog of any size shrinks every read, solanaListPages pages of
// listing or solanaReads transactions at a time, and the cursor only ever
// moves past what was read. A first read (until "") takes only
// transactions from since on: an account opened by a payment is read from
// its first transaction, an old wallet's history never. A call that fails
// ends the read there; it is an error only if nothing came before it.
func (r *Reader) Solana(ctx context.Context, rail Rail, owner, account, cursor string, since time.Time) ([]Transfer, string, error) {
	endpoint := r.endpoint(rail)
	type sig struct {
		Signature string `json:"signature"`
		Err       any    `json:"err"`
		BlockTime *int64 `json:"blockTime"`
	}
	until, frontier, _ := strings.Cut(cursor, "|")
	with := func(at, front string) string {
		if front == "" {
			return at
		}
		return at + "|" + front
	}
	var sigs []sig
	before := frontier
	reached := false
	for range solanaListPages {
		cfg := map[string]any{"limit": 1000, "commitment": "finalized"}
		if until != "" {
			cfg["until"] = until
		}
		if before != "" {
			cfg["before"] = before
		}
		var page []sig
		if err := r.rpc(ctx, endpoint, "getSignaturesForAddress", []any{account, cfg}, &page); err != nil {
			if len(sigs) == 0 {
				return nil, cursor, err
			}
			return nil, with(until, before), nil
		}
		sigs = append(sigs, page...)
		last := len(page) - 1
		// A first read needs nothing older than since.
		if len(page) < 1000 || (until == "" && page[last].BlockTime != nil && time.Unix(*page[last].BlockTime, 0).Before(since)) {
			reached = true
			break
		}
		before = page[last].Signature
	}
	if !reached {
		// Listing alone filled this read: the next goes on from here.
		return nil, with(until, before), nil
	}
	var out []Transfer
	at, fetched, done := until, 0, true
	// Oldest first, so what arrived is recorded in the order it came and the
	// cursor only ever moves past what was read.
	for i := len(sigs) - 1; i >= 0; i-- {
		s := sigs[i]
		if fetched == solanaReads {
			done = false
			break
		}
		if s.Err != nil || (until == "" && (s.BlockTime == nil || time.Unix(*s.BlockTime, 0).Before(since))) {
			at = s.Signature
			continue
		}
		t, err := r.received(ctx, endpoint, rail, owner, s.Signature)
		if err != nil {
			if at == until {
				return nil, cursor, err
			}
			done = false
			break
		}
		fetched++
		at = s.Signature
		if t != nil {
			if s.BlockTime != nil {
				t.At = time.Unix(*s.BlockTime, 0)
			}
			out = append(out, *t)
		}
	}
	// Everything listed up to the frontier was read: the next read lists
	// from the newest again. Otherwise it keeps the frontier.
	if done {
		return out, at, nil
	}
	return out, with(at, frontier), nil
}

// received is what owner gained on rail in the transaction sig, or nil if
// nothing: the change in owner's balance of the mint across its token
// accounts, or in its SOL, from before the transaction to after.
func (r *Reader) received(ctx context.Context, endpoint string, rail Rail, owner, sig string) (*Transfer, error) {
	type tokenBalance struct {
		Index  int    `json:"accountIndex"`
		Mint   string `json:"mint"`
		Owner  string `json:"owner"`
		Amount struct {
			Amount string `json:"amount"`
		} `json:"uiTokenAmount"`
	}
	var tx *struct {
		Meta *struct {
			Err  any            `json:"err"`
			Pre  []uint64       `json:"preBalances"`
			Post []uint64       `json:"postBalances"`
			PreT []tokenBalance `json:"preTokenBalances"`
			PstT []tokenBalance `json:"postTokenBalances"`
		} `json:"meta"`
		Transaction struct {
			Message struct {
				Keys []struct {
					Pubkey string `json:"pubkey"`
					Signer bool   `json:"signer"`
				} `json:"accountKeys"`
			} `json:"message"`
		} `json:"transaction"`
	}
	if err := r.rpc(ctx, endpoint, "getTransaction", []any{sig, map[string]any{"encoding": "jsonParsed", "commitment": "finalized", "maxSupportedTransactionVersion": 0}}, &tx); err != nil {
		return nil, err
	}
	if tx == nil || tx.Meta == nil {
		// Finalized in the signature list but not served yet: the read
		// fails and the cursor stays, so it is read again.
		return nil, fmt.Errorf("getTransaction: %s not available yet", sig)
	}
	if tx.Meta.Err != nil {
		return nil, nil
	}
	from := ""
	for _, k := range tx.Transaction.Message.Keys {
		if k.Signer {
			from = k.Pubkey
			break
		}
	}
	delta := new(big.Int)
	if rail.Token == "" {
		for i, k := range tx.Transaction.Message.Keys {
			if k.Pubkey == owner && i < len(tx.Meta.Pre) && i < len(tx.Meta.Post) {
				delta.Sub(new(big.Int).SetUint64(tx.Meta.Post[i]), new(big.Int).SetUint64(tx.Meta.Pre[i]))
			}
		}
	} else {
		sum := func(bs []tokenBalance, sign int64) error {
			for _, b := range bs {
				if b.Mint != rail.Token || b.Owner != owner {
					continue
				}
				v, ok := new(big.Int).SetString(b.Amount.Amount, 10)
				if !ok {
					return fmt.Errorf("getTransaction: %q is not an amount", b.Amount.Amount)
				}
				delta.Add(delta, v.Mul(v, big.NewInt(sign)))
			}
			return nil
		}
		if err := sum(tx.Meta.PstT, 1); err != nil {
			return nil, err
		}
		if err := sum(tx.Meta.PreT, -1); err != nil {
			return nil, err
		}
	}
	if delta.Sign() <= 0 {
		return nil, nil
	}
	return &Transfer{Tx: sig, Amount: delta, From: from}, nil
}

// priceBand is the US dollar range a quoted coin's price must fall in to be
// believed: far wider than it moves, far narrower than a bad answer. A
// checkout priced from a wrong spot price sells months for change.
var priceBand = map[string][2]int64{"SOL": {1, 10_000}}

// decimal is a plain decimal number: no exponent, no fraction.
var decimal = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)

// USD is what one coin of asset ("SOL") is worth in US dollars now:
// Coinbase's public spot price, which takes no key.
func (r *Reader) USD(ctx context.Context, asset string) (*big.Rat, error) {
	base := r.Prices
	if base == "" {
		base = "https://api.coinbase.com"
	}
	raw, err := r.do(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/v2/prices/"+url.PathEscape(asset)+"-USD/spot", nil, nil)
	if err != nil {
		return nil, fmt.Errorf("price: %w", err)
	}
	var res struct {
		Data struct {
			Amount   string `json:"amount"`
			Currency string `json:"currency"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("price: %w", err)
	}
	band, known := priceBand[asset]
	p, ok := new(big.Rat).SetString(res.Data.Amount)
	if !known || !decimal.MatchString(res.Data.Amount) || !ok || res.Data.Currency != "USD" ||
		p.Cmp(new(big.Rat).SetInt64(band[0])) < 0 || p.Cmp(new(big.Rat).SetInt64(band[1])) > 0 {
		return nil, fmt.Errorf("price: %q %q is not a price for %s", res.Data.Amount, res.Data.Currency, asset)
	}
	return p, nil
}

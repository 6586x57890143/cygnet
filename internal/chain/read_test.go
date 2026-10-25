package chain

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// node is a fake JSON-RPC node: each method answers from answer, which
// sees the call's params; a method it has no answer for is an RPC error.
func node(t *testing.T, answer map[string]func(params []json.RawMessage) any) (*httptest.Server, *[]string) {
	t.Helper()
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		calls = append(calls, req.Method)
		fn, ok := answer[req.Method]
		if !ok {
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "error": map[string]any{"code": -32601, "message": "no " + req.Method}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": fn(req.Params)})
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

const wallet = "0x1111111111111111111111111111111111111111"

func padded(addr string) string {
	return "0x" + strings.Repeat("0", 24) + strings.TrimPrefix(addr, "0x")
}

func TestEVMReadsFinalTransfersIntoTheWalletOnly(t *testing.T) {
	base := rail(t, "base:USDC")
	var asked []map[string]any
	srv, _ := node(t, map[string]func([]json.RawMessage) any{
		"eth_blockNumber": func([]json.RawMessage) any { return "0x64" }, // 100
		"eth_getLogs": func(p []json.RawMessage) any {
			var f map[string]any
			_ = json.Unmarshal(p[0], &f)
			asked = append(asked, f)
			log := func(tx string, idx, amount int, to string, removed bool, token string) map[string]any {
				return map[string]any{"address": token, "topics": []string{transferTopic, padded("0x2222222222222222222222222222222222222222"), padded(to)},
					"data": fmt.Sprintf("0x%x", amount), "transactionHash": tx, "logIndex": fmt.Sprintf("0x%x", idx), "removed": removed}
			}
			return []any{
				log("0xAA", 3, 5_004_217, wallet, false, strings.ToLower(base.Token)),
				log("0xbb", 1, 9, wallet, true, base.Token),                                        // reorged
				log("0xcc", 1, 9, "0x3333333333333333333333333333333333333333", false, base.Token), // not ours
				log("0xdd", 1, 9, wallet, false, "0x4444444444444444444444444444444444444444"),     // another token
				log("0xee", 1, 0, wallet, false, base.Token),                                       // nothing
			}
		},
	})
	r := &Reader{RPC: map[string]string{"base": srv.URL}}
	ctx := context.Background()

	got, next, err := r.EVM(ctx, base, wallet, 0)
	if err != nil || len(got) != 0 || next != 71 {
		t.Fatalf("first read: %v %d %v; want nothing and start at block 71 (100 less 30, plus 1)", got, next, err)
	}
	got, next, err = r.EVM(ctx, base, wallet, 60)
	if err != nil || next != 71 {
		t.Fatalf("read: %d %v", next, err)
	}
	if len(got) != 1 || got[0].Tx != "0xaa" || got[0].Idx != 3 || got[0].Amount.Int64() != 5_004_217 || got[0].From != "0x2222222222222222222222222222222222222222" {
		t.Fatalf("transfers %+v", got)
	}
	f := asked[len(asked)-1]
	if f["fromBlock"] != "0x3c" || f["toBlock"] != "0x46" || f["address"] != base.Token {
		t.Fatalf("filter %v", f)
	}
	topics := f["topics"].([]any)
	if topics[0] != transferTopic || topics[1] != nil || topics[2] != padded(wallet) {
		t.Fatalf("topics %v", topics)
	}
	// Caught up: nothing past the final block is asked for.
	n := len(asked)
	if _, next, err := r.EVM(ctx, base, wallet, 71); err != nil || next != 71 || len(asked) != n {
		t.Fatalf("caught up: %d %v, asked %d more", next, err, len(asked)-n)
	}
	if _, _, err := r.EVM(ctx, base, "0xnope", 1); err == nil {
		t.Fatal("read a wallet that isn't an address")
	}
}

func TestEVMCatchesUpInSpans(t *testing.T) {
	poly := rail(t, "polygon:USDC")
	var spansAsked [][2]string
	srv, _ := node(t, map[string]func([]json.RawMessage) any{
		"eth_blockNumber": func([]json.RawMessage) any { return fmt.Sprintf("0x%x", 100_000+poly.Confirmations) },
		"eth_getLogs": func(p []json.RawMessage) any {
			var f map[string]string
			_ = json.Unmarshal(p[0], &f)
			spansAsked = append(spansAsked, [2]string{f["fromBlock"], f["toBlock"]})
			return []any{}
		},
	})
	r := &Reader{RPC: map[string]string{"polygon": srv.URL}}
	_, next, err := r.EVM(context.Background(), poly, wallet, 1)
	if err != nil || next != 1+spans*span || len(spansAsked) != spans || spansAsked[0] != [2]string{"0x1", "0x3e8"} {
		t.Fatalf("next %d %v, spans %v", next, err, spansAsked)
	}
}

func TestEVMFailuresKeepTheCursor(t *testing.T) {
	base := rail(t, "base:USDC")
	srv, _ := node(t, map[string]func([]json.RawMessage) any{"eth_blockNumber": func([]json.RawMessage) any { return "0x64" }})
	r := &Reader{RPC: map[string]string{"base": srv.URL}}
	if _, next, err := r.EVM(context.Background(), base, wallet, 50); err == nil || next != 50 || !strings.Contains(err.Error(), "no eth_getLogs") {
		t.Fatalf("a refused getLogs: %d %v", next, err)
	}
	young, _ := node(t, map[string]func([]json.RawMessage) any{"eth_blockNumber": func([]json.RawMessage) any { return "0x5" }})
	r.RPC["base"] = young.URL
	if got, next, err := r.EVM(context.Background(), base, wallet, 0); err != nil || got != nil || next != 0 {
		t.Fatalf("a chain younger than its confirmations: %d %v", next, err)
	}
	bad, _ := node(t, map[string]func([]json.RawMessage) any{"eth_blockNumber": func([]json.RawMessage) any { return "64" }})
	r.RPC["base"] = bad.URL
	if _, _, err := r.EVM(context.Background(), base, wallet, 0); err == nil {
		t.Fatal("read a block number that isn't hex")
	}
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer down.Close()
	r.RPC["base"] = down.URL + "/key-in-path"
	_, _, err := r.EVM(context.Background(), base, wallet, 0)
	if err == nil || strings.Contains(err.Error(), "key-in-path") {
		t.Fatalf("a node that's down: %v", err)
	}
}

const tronWallet = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"

func TestTronReadsConfirmedTransfersPageByPage(t *testing.T) {
	usdt := rail(t, "tron:USDT")
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		if r.URL.Path != "/v1/accounts/"+tronWallet+"/transactions/trc20" || r.Header.Get("TRON-PRO-API-KEY") != "k" {
			http.NotFound(w, r)
			return
		}
		item := func(tx string, at int64, to, token, kind, value string) map[string]any {
			return map[string]any{"transaction_id": tx, "token_info": map[string]any{"address": token, "decimals": 6}, "block_timestamp": at, "from": "TSender", "to": to, "type": kind, "value": value}
		}
		if r.URL.Query().Get("fingerprint") == "" {
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []any{
				item("t1", 1000, tronWallet, usdt.Token, "Transfer", "5004217"),
				item("t2", 1100, tronWallet, "TOther", "Transfer", "1"),
			}, "meta": map[string]any{"fingerprint": "fp"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []any{
			item("t3", 1200, tronWallet, usdt.Token, "Approval", "1"),
			item("t4", 1300, tronWallet, usdt.Token, "Transfer", "7"),
		}, "meta": map[string]any{}})
	}))
	defer srv.Close()
	r := &Reader{RPC: map[string]string{"tron": srv.URL}, TronKey: "k"}
	ctx := context.Background()
	now := time.UnixMilli(5000)
	if got, next, err := r.Tron(ctx, usdt, tronWallet, 0, now); err != nil || got != nil || next != 5000 || len(queries) != 0 {
		t.Fatalf("first read: %v %d %v", got, next, err)
	}
	got, next, err := r.Tron(ctx, usdt, tronWallet, 900, now)
	if err != nil || next != 1300 {
		t.Fatalf("read: %d %v", next, err)
	}
	if len(got) != 2 || got[0].Tx != "t1" || got[0].Amount.Int64() != 5004217 || got[0].From != "TSender" || got[1].Tx != "t4" {
		t.Fatalf("transfers %+v", got)
	}
	for _, want := range []string{"only_confirmed=true", "only_to=true", "min_timestamp=900", "order_by=block_timestamp%2Casc", "contract_address=" + usdt.Token} {
		if !strings.Contains(queries[0], want) {
			t.Errorf("query %s lacks %s", queries[0], want)
		}
	}
	if !strings.Contains(queries[1], "fingerprint=fp") {
		t.Fatalf("second page %s", queries[1])
	}
	if _, _, err := r.Tron(ctx, usdt, "Tnope", 900, now); err == nil {
		t.Fatal("read a wallet that isn't an address")
	}
	r.TronKey = ""
	if _, next, err := r.Tron(ctx, usdt, tronWallet, 900, now); err == nil || next != 900 {
		t.Fatalf("a refused read moved the cursor: %d %v", next, err)
	}
}

func TestTronRefusesAnAnswerThatIsntOne(t *testing.T) {
	usdt := rail(t, "tron:USDT")
	for _, body := range []string{`{"success":false}`, `not json`, `{"success":true,"data":[{"type":"Transfer","to":"` + tronWallet + `","token_info":{"address":"` + usdt.Token + `"},"value":"x"}]}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }))
		r := &Reader{RPC: map[string]string{"tron": srv.URL}}
		if _, next, err := r.Tron(context.Background(), usdt, tronWallet, 900, time.Now()); err == nil || next != 900 {
			t.Errorf("%s: %d %v", body, next, err)
		}
		srv.Close()
	}
}

const (
	owner   = "mvines9iiHiQTysrwkJjGf2gb9Ex9jXJX8ns3qwf2kN"
	account = "4Nd1mBQtrMJVYVfKf2PJy9NZUZdTAsp7D4xWLs4gDB4T"
)

// solanaNode answers for a token account with three finalized signatures,
// newest first: s3 a payment of 5004217 to owner, s2 a failed one, s1
// before the cursor's start. A getTransaction for s4 is not served yet.
func solanaNode(t *testing.T, usdc Rail) (*httptest.Server, *[]string) {
	return node(t, map[string]func([]json.RawMessage) any{
		"getTokenAccountsByOwner": func(p []json.RawMessage) any {
			return map[string]any{"value": []any{map[string]any{"pubkey": account}, map[string]any{"pubkey": "2"}}}
		},
		"getSignaturesForAddress": func(p []json.RawMessage) any {
			var cfg map[string]any
			_ = json.Unmarshal(p[1], &cfg)
			if cfg["until"] == "s3" {
				return []any{map[string]any{"signature": "s4", "err": nil, "blockTime": 2000}}
			}
			return []any{
				map[string]any{"signature": "s3", "err": nil, "blockTime": 2000},
				map[string]any{"signature": "s2", "err": map[string]any{"x": 1}, "blockTime": 1900},
				map[string]any{"signature": "s1", "err": nil, "blockTime": 500},
			}
		},
		"getTransaction": func(p []json.RawMessage) any {
			var sig string
			_ = json.Unmarshal(p[0], &sig)
			bal := func(idx int, owner, amount string) map[string]any {
				return map[string]any{"accountIndex": idx, "mint": usdc.Token, "owner": owner, "uiTokenAmount": map[string]any{"amount": amount, "decimals": 6}}
			}
			keys := []any{map[string]any{"pubkey": "Payer", "signer": true}, map[string]any{"pubkey": account, "signer": false}, map[string]any{"pubkey": owner, "signer": false}}
			switch sig {
			case "s3":
				return map[string]any{"meta": map[string]any{"err": nil, "preBalances": []int{9, 1, 1}, "postBalances": []int{8, 1, 1},
					"preTokenBalances":  []any{bal(0, "Payer", "9000000"), bal(1, owner, "10")},
					"postTokenBalances": []any{bal(0, "Payer", "3995783"), bal(1, owner, "5004227")}}, "transaction": map[string]any{"message": map[string]any{"accountKeys": keys}}}
			case "s1":
				t.Error("read a transaction from before the cursor started")
			}
			return nil
		},
	})
}

func TestSolanaReadsWhatArrivedInFinalizedTransactions(t *testing.T) {
	usdc := rail(t, "solana:USDC")
	srv, calls := solanaNode(t, usdc)
	r := &Reader{RPC: map[string]string{"solana": srv.URL}}
	ctx := context.Background()
	accts, err := r.Accounts(ctx, usdc, owner)
	if err != nil || strings.Join(accts, ",") != "2,"+account {
		t.Fatalf("accounts %v %v", accts, err)
	}
	got, newest, err := r.Solana(ctx, usdc, owner, account, "", time.Unix(1000, 0))
	if err != nil || newest != "s3" {
		t.Fatalf("read: %s %v", newest, err)
	}
	if len(got) != 1 || got[0].Tx != "s3" || got[0].Amount.Int64() != 5_004_217 || got[0].From != "Payer" {
		t.Fatalf("transfers %+v", got)
	}
	// s4 isn't served yet: the read fails and the cursor stays where it was.
	if _, newest, err := r.Solana(ctx, usdc, owner, account, "s3", time.Unix(1000, 0)); err == nil || newest != "s3" {
		t.Fatalf("a transaction not served yet: %s %v", newest, err)
	}
	if n := strings.Count(strings.Join(*calls, " "), "getTransaction"); n != 2 {
		t.Fatalf("%d transactions read, want s3 and s4", n)
	}
	if a, err := r.Accounts(ctx, rail(t, "solana:SOL"), owner); err != nil || len(a) != 1 || a[0] != owner {
		t.Fatalf("SOL lands in the wallet itself: %v %v", a, err)
	}
	if _, err := r.Accounts(ctx, usdc, "nope"); err == nil {
		t.Fatal("accounts of a wallet that isn't a key")
	}
}

func TestSolanaSOLIsTheWalletsOwnBalanceChange(t *testing.T) {
	sol := rail(t, "solana:SOL")
	srv, _ := node(t, map[string]func([]json.RawMessage) any{
		"getSignaturesForAddress": func([]json.RawMessage) any {
			return []any{
				map[string]any{"signature": "in", "err": nil, "blockTime": 2000},
				map[string]any{"signature": "out", "err": nil, "blockTime": 1999},
			}
		},
		"getTransaction": func(p []json.RawMessage) any {
			var sig string
			_ = json.Unmarshal(p[0], &sig)
			keys := []any{map[string]any{"pubkey": "Payer", "signer": true}, map[string]any{"pubkey": owner, "signer": sig == "out"}}
			pre, post := []int{100_000_000, 5}, []int{66_655_000, 33_340_022}
			if sig == "out" {
				pre, post = []int{1, 100}, []int{1, 50}
			}
			return map[string]any{"meta": map[string]any{"err": nil, "preBalances": pre, "postBalances": post}, "transaction": map[string]any{"message": map[string]any{"accountKeys": keys}}}
		},
	})
	r := &Reader{RPC: map[string]string{"solana": srv.URL}}
	got, newest, err := r.Solana(context.Background(), sol, owner, owner, "", time.Unix(0, 0))
	if err != nil || newest != "in" || len(got) != 1 || got[0].Tx != "in" || got[0].Amount.Int64() != 33_340_017 {
		t.Fatalf("got %+v %s %v", got, newest, err)
	}
}

func TestSolanaWithNothingNewKeepsTheCursor(t *testing.T) {
	usdc := rail(t, "solana:USDC")
	srv, _ := node(t, map[string]func([]json.RawMessage) any{"getSignaturesForAddress": func([]json.RawMessage) any { return []any{} }})
	r := &Reader{RPC: map[string]string{"solana": srv.URL}}
	if got, newest, err := r.Solana(context.Background(), usdc, owner, account, "s9", time.Now()); err != nil || got != nil || newest != "s9" {
		t.Fatalf("%v %s %v", got, newest, err)
	}
	down, _ := node(t, nil)
	r.RPC["solana"] = down.URL
	if _, newest, err := r.Solana(context.Background(), usdc, owner, account, "s9", time.Now()); err == nil || newest != "s9" {
		t.Fatalf("a refused read: %s %v", newest, err)
	}
}

func TestUSDIsCoinbasesSpotPrice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/prices/SOL-USD/spot":
			_, _ = io.WriteString(w, `{"data":{"amount":"152.37","base":"SOL","currency":"USD"}}`)
		case "/v2/prices/BAD-USD/spot":
			_, _ = io.WriteString(w, `{"data":{"amount":"0","currency":"USD"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	r := &Reader{Prices: srv.URL}
	p, err := r.USD(context.Background(), "SOL")
	if err != nil || p.FloatString(2) != "152.37" {
		t.Fatalf("price %v %v", p, err)
	}
	for _, asset := range []string{"BAD", "NONE"} {
		if _, err := r.USD(context.Background(), asset); err == nil {
			t.Errorf("%s: a price", asset)
		}
	}
	// A price far outside the coin's band, or not a plain decimal, is refused.
	for _, amount := range []string{"150000", "0.5", "1e3", "300/2"} {
		bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"data":{"amount":"`+amount+`","currency":"USD"}}`)
		}))
		if _, err := (&Reader{Prices: bad.URL}).USD(context.Background(), "SOL"); err == nil {
			t.Errorf("%s: a price", amount)
		}
		bad.Close()
	}
}

func TestSolanaMovesItsCursorOnlyPastWhatItRead(t *testing.T) {
	usdc := rail(t, "solana:USDC")
	srv, _ := node(t, map[string]func([]json.RawMessage) any{
		"getSignaturesForAddress": func([]json.RawMessage) any {
			return []any{
				map[string]any{"signature": "n3", "err": nil, "blockTime": 2000},
				map[string]any{"signature": "n2", "err": nil, "blockTime": 2000},
				map[string]any{"signature": "n1", "err": nil, "blockTime": 2000},
			}
		},
		"getTransaction": func(p []json.RawMessage) any {
			var sig string
			_ = json.Unmarshal(p[0], &sig)
			if sig == "n1" {
				return map[string]any{"meta": map[string]any{"err": nil, "preBalances": []int{1}, "postBalances": []int{1}}, "transaction": map[string]any{"message": map[string]any{"accountKeys": []any{}}}}
			}
			return nil // n2 fails to read
		},
	})
	r := &Reader{RPC: map[string]string{"solana": srv.URL}}
	got, at, err := r.Solana(context.Background(), usdc, owner, account, "n0", time.Unix(1000, 0))
	if err != nil || at != "n1" || len(got) != 0 {
		t.Fatalf("read %v, cursor %s, %v; want the cursor on n1, before the one that failed", got, at, err)
	}
}

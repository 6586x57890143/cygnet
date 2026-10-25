package support

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/chain"
	"github.com/6586x57890143/cygnet/internal/core"
	"github.com/6586x57890143/cygnet/internal/core/coretest"
	"github.com/6586x57890143/cygnet/internal/store"
	"github.com/6586x57890143/cygnet/link"
)

var t0 = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

const (
	evmWallet    = "0x1111111111111111111111111111111111111111"
	tronWallet   = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"
	solanaWallet = "mvines9iiHiQTysrwkJjGf2gb9Ex9jXJX8ns3qwf2kN"
	usdcAccount  = "4Nd1mBQtrMJVYVfKf2PJy9NZUZdTAsp7D4xWLs4gDB4T"
	keeper       = snowflake.ID(5)
)

// supports is the store on CYGNET_TEST_DATABASE_URL with every rail's
// cursor cleared, since the rails are the real ones and other runs read
// them too.
func supports(t *testing.T) *store.Support {
	t.Helper()
	dsn := os.Getenv("CYGNET_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("CYGNET_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "delete from support_cursor"); err != nil {
		t.Fatal(err)
	}
	return store.NewSupport(pool)
}

// someone is a person no other test or run shares.
func someone() snowflake.ID { return snowflake.ID(rand.Int64N(1<<62) + 1<<60) }

// running is the module with a database, the wallets w, its nodes at
// node, its clock at t0 and 5 its keeper.
func running(t *testing.T, w Wallets, node string) *Module {
	t.Helper()
	r := &chain.Reader{RPC: map[string]string{"base": node, "polygon": node, "solana": node, "tron": node}, Prices: node}
	m := New(supports(t), r, w, 9, keeper, "https://cygnet.lol/support", func(context.Context, snowflake.ID) (Owner, error) {
		return Owner{Name: "steve", Avatar: "https://cdn.discordapp.com/embed/avatars/0.png"}, nil
	}, nil)
	m.now = func() time.Time { return t0 }
	return m
}

func ask(t *testing.T, fn func(context.Context, string, json.RawMessage) (any, error), caller string, body any) (any, error) {
	t.Helper()
	raw, _ := json.Marshal(body)
	return fn(context.Background(), caller, raw)
}

func denied(err error, reason string) bool {
	le, ok := errors.AsType[*link.Error](err)
	return ok && le.Code == link.Denied && le.Reason == reason
}

func TestWalletsAreCheckedByFamily(t *testing.T) {
	if err := (Wallets{EVM: evmWallet, Tron: tronWallet, Solana: solanaWallet}).Check(); err != nil {
		t.Fatal(err)
	}
	for _, w := range []Wallets{{EVM: "0xnope"}, {Tron: "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6u"}, {Solana: tronWallet}} {
		if err := w.Check(); err == nil {
			t.Errorf("%+v passed", w)
		}
	}
}

func TestOfferIsWhatCanBePaidAndWhoItGoesTo(t *testing.T) {
	m := running(t, Wallets{EVM: evmWallet, Tron: tronWallet}, "http://unused")
	out, err := ask(t, m.offer, edgeBird, nil)
	if err != nil {
		t.Fatal(err)
	}
	o := out.(Offer)
	if len(o.Plans) != 4 || o.Plans[3].Months != 12 || o.Plans[3].Prices[500] != 5000 || o.Plans[1].Prices[1000] != 2800 || fmt.Sprint(o.Rates) != "[500 1000 2000]" {
		t.Fatalf("offer %+v", o)
	}
	var rails []string
	for _, r := range o.Rails {
		rails = append(rails, r.Key)
	}
	if strings.Join(rails, " ") != "base:USDC polygon:USDC tron:USDT" {
		t.Fatalf("rails %v: only those with a wallet, in the table's order", rails)
	}
	if o.Owner != (Owner{ID: "9", Name: "steve", Avatar: "https://cdn.discordapp.com/embed/avatars/0.png"}) {
		t.Fatalf("owner %+v", o.Owner)
	}
	if _, err := ask(t, m.offer, "merlin", nil); err == nil {
		t.Fatal("a bird asked for the offer")
	}
	none := running(t, Wallets{}, "http://unused")
	if _, err := ask(t, none.offer, edgeBird, nil); !denied(err, NoRails) {
		t.Fatalf("no wallets: %v", err)
	}
	if _, err := ask(t, New(nil, nil, Wallets{EVM: evmWallet}, 9, 0, "", nil, nil).offer, edgeBird, nil); err == nil {
		t.Fatal("an offer without a database")
	}
}

func TestTheOwnersProfileIsReadHourlyAndSurvivesFailing(t *testing.T) {
	calls := 0
	fail := false
	m := New(nil, nil, Wallets{}, 9, 0, "", func(context.Context, snowflake.ID) (Owner, error) {
		calls++
		if fail {
			return Owner{}, errors.New("discord is down")
		}
		return Owner{ID: "ignored", Name: "steve"}, nil
	}, nil)
	now := t0
	m.now = func() time.Time { return now }
	if o := m.ownerProfile(context.Background()); o.ID != "9" || o.Name != "steve" {
		t.Fatalf("owner %+v", o)
	}
	m.ownerProfile(context.Background())
	if calls != 1 {
		t.Fatalf("read %d times in an hour", calls)
	}
	now = now.Add(2 * time.Hour)
	fail = true
	// A failed read keeps the name it had and isn't retried for five minutes.
	if o := m.ownerProfile(context.Background()); o.ID != "9" || o.Name != "steve" || calls != 2 {
		t.Fatalf("after a failed read: %+v, %d calls", o, calls)
	}
	m.ownerProfile(context.Background())
	if calls != 2 {
		t.Fatalf("retried a failed read at once: %d calls", calls)
	}
	now = now.Add(6 * time.Minute)
	m.ownerProfile(context.Background())
	if calls != 3 {
		t.Fatalf("never retried a failed read: %d calls", calls)
	}
}

func TestACheckoutIsQuotedOnItsRailAndOnlyItsOwnerSeesIt(t *testing.T) {
	m := running(t, Wallets{EVM: evmWallet, Solana: solanaWallet}, "http://unused")
	user := someone()
	out, err := ask(t, m.quote, edgeBird, Ask{User: user.String(), Months: 3, Cents: 1000, Rail: "solana:USDC"})
	if err != nil {
		t.Fatal(err)
	}
	c := out.(Checkout)
	if c.State != store.InvoiceOpen || c.Price != 2800 || !c.Gold || c.To != solanaWallet || c.Label != "usdc on solana" || !strings.HasPrefix(c.Amount, "28.00") || c.Expires != t0.Add(time.Hour).UnixMilli() {
		t.Fatalf("checkout %+v", c)
	}
	if !strings.HasPrefix(c.Pay, "solana:"+solanaWallet+"?") || !strings.Contains(c.Pay, "amount="+c.Amount) {
		t.Fatalf("pay link %s", c.Pay)
	}
	got, err := ask(t, m.checkout, edgeBird, Ask{User: user.String(), ID: c.ID})
	if err != nil || got.(Checkout).Units != c.Units {
		t.Fatalf("reading it back: %+v %v", got, err)
	}
	if _, err := ask(t, m.checkout, edgeBird, Ask{User: someone().String(), ID: c.ID}); !denied(err, NotYours) {
		t.Fatalf("someone else's: %v", err)
	}
	for _, id := range []string{"", "x", strings.Repeat("a", 26), strings.Repeat("A", 26), "../../../../etc/passwd/aaaa"} {
		if _, err := ask(t, m.checkout, edgeBird, Ask{User: user.String(), ID: id}); !denied(err, NoCheckout) {
			t.Errorf("checkout %q: %v", id, err)
		}
	}
	evm, err := ask(t, m.quote, edgeBird, Ask{User: user.String(), Months: 1, Cents: 500, Rail: "base:USDC"})
	if err != nil || evm.(Checkout).Pay != "" || evm.(Checkout).To != evmWallet || evm.(Checkout).Gold {
		t.Fatalf("a base checkout: %+v %v", evm, err)
	}
	for _, a := range []Ask{
		{User: user.String(), Months: 1, Cents: 500, Rail: "tron:USDT"},    // no tron wallet
		{User: user.String(), Months: 1, Cents: 500, Rail: "ethereum:ETH"}, // no such rail
		{User: user.String(), Months: 2, Cents: 500, Rail: "base:USDC"},    // no such length
		{User: user.String(), Months: 1, Cents: 1, Rail: "base:USDC"},      // no such rate
	} {
		if _, err := ask(t, m.quote, edgeBird, a); !denied(err, NotOffered) {
			t.Errorf("%+v: %v", a, err)
		}
	}
	if _, err := ask(t, m.quote, edgeBird, Ask{User: user.String(), Months: 12, Cents: 500, Rail: "base:USDC"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ask(t, m.quote, edgeBird, Ask{User: user.String(), Months: 6, Cents: 500, Rail: "base:USDC"}); !denied(err, TooMany) {
		t.Fatalf("a fourth checkout: %v", err)
	}
	for _, bad := range []any{Ask{Months: 1, Cents: 500, Rail: "base:USDC"}, Ask{User: "steve", Rail: "base:USDC"}, "not an ask"} {
		if _, err := ask(t, m.quote, edgeBird, bad); err == nil {
			t.Errorf("%v: quoted", bad)
		}
	}
	if _, err := ask(t, m.quote, "merlin", Ask{User: user.String(), Months: 1, Cents: 500, Rail: "base:USDC"}); err == nil {
		t.Fatal("a bird opened a checkout")
	}
}

// prices is Coinbase's spot price as a test sets it: 150 for SOL, or a
// refusal.
func prices(t *testing.T, up *bool) *httptest.Server {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !*up || r.URL.Path != "/v2/prices/SOL-USD/spot" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"amount":"150.00","currency":"USD"}}`))
	}))
	t.Cleanup(s.Close)
	return s
}

func TestSOLIsQuotedAtTodaysPriceForTwentyMinutes(t *testing.T) {
	up := true
	p := prices(t, &up)
	m := running(t, Wallets{Solana: solanaWallet}, p.URL)
	user := someone()
	out, err := ask(t, m.quote, edgeBird, Ask{User: user.String(), Months: 1, Cents: 500, Rail: "solana:SOL"})
	if err != nil {
		t.Fatal(err)
	}
	c := out.(Checkout)
	if c.Quote != "150.00" || !strings.HasPrefix(c.Amount, "0.0334") || c.Expires != t0.Add(20*time.Minute).UnixMilli() || strings.Contains(c.Pay, "spl-token") {
		t.Fatalf("sol checkout %+v", c)
	}
	up = false
	// The price is shared for priceFor, and an open checkout comes back
	// without one at all.
	m.now = func() time.Time { return t0.Add(priceFor + time.Second) }
	again, err := ask(t, m.quote, edgeBird, Ask{User: user.String(), Months: 1, Cents: 500, Rail: "solana:SOL"})
	if err != nil || again.(Checkout).ID != c.ID {
		t.Fatalf("the open checkout with the feed down: %+v %v", again, err)
	}
	if _, err := ask(t, m.quote, edgeBird, Ask{User: someone().String(), Months: 1, Cents: 500, Rail: "solana:SOL"}); err == nil {
		t.Fatal("quoted SOL with no price")
	} else if le, ok := errors.AsType[*link.Error](err); !ok || le.Code != link.Busy {
		t.Fatalf("no price: %v", err)
	}
}

func TestBirdsAskWhoBacksThem(t *testing.T) {
	m := running(t, Wallets{EVM: evmWallet}, "http://unused")
	user := someone()
	if _, err := m.store.Gift(context.Background(), user, 2, true, keeper, t0.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	out, err := ask(t, m.supporter, "merlin", Ask{User: user.String()})
	if err != nil {
		t.Fatal(err)
	}
	if st := out.(Status); !st.Active || !st.Gold || st.Until != t0.Add(-time.Hour).AddDate(0, 2, 0).UnixMilli() {
		t.Fatalf("status %+v", st)
	}
	if out, _ := ask(t, m.supporter, "merlin", Ask{User: someone().String()}); out.(Status) != (Status{}) {
		t.Fatalf("someone who never did: %+v", out)
	}
	if _, err := ask(t, m.supporter, "merlin", Ask{}); err == nil {
		t.Fatal("a status for nobody")
	}
	all, err := ask(t, m.supporters, "peregrine", nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range all.(map[string][]Backer)["supporters"] {
		found = found || (b.User == user.String() && b.Gold)
	}
	if !found {
		t.Fatalf("%s not among %v", user, all)
	}
}

// evmNode is a chain at head, whose logs are whatever it holds when asked.
type evmNode struct {
	mu   sync.Mutex
	head uint64
	logs []map[string]any
	gets int
}

func (n *evmNode) serve(t *testing.T) *httptest.Server {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.mu.Lock()
		defer n.mu.Unlock()
		var req struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var result any
		switch req.Method {
		case "eth_blockNumber":
			result = fmt.Sprintf("0x%x", n.head)
		case "eth_getLogs":
			n.gets++
			var f map[string]any
			_ = json.Unmarshal(req.Params[0], &f)
			var out []map[string]any
			for _, l := range n.logs {
				if strings.EqualFold(l["address"].(string), f["address"].(string)) {
					out = append(out, l)
				}
			}
			result = out
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
	}))
	t.Cleanup(s.Close)
	return s
}

func transferLog(token, to string, amount *big.Int, tx string) map[string]any {
	pad := func(a string) string { return "0x" + strings.Repeat("0", 24) + strings.TrimPrefix(a, "0x") }
	return map[string]any{
		"address": token, "topics": []string{"0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef", pad("0x2222222222222222222222222222222222222222"), pad(to)},
		"data": "0x" + amount.Text(16), "transactionHash": tx, "logIndex": "0x0", "removed": false,
	}
}

func TestAPaymentOnBaseIsSeenAndBuysItsBacking(t *testing.T) {
	n := &evmNode{head: 100}
	s := n.serve(t)
	m := running(t, Wallets{EVM: evmWallet}, s.URL)
	ctx := context.Background()
	user := someone()
	out, err := ask(t, m.quote, edgeBird, Ask{User: user.String(), Months: 3, Cents: 500, Rail: "base:USDC"})
	if err != nil {
		t.Fatal(err)
	}
	c := out.(Checkout)
	base, _ := chain.Find("base:USDC")
	units, _ := new(big.Int).SetString(c.Units, 10)
	tx := fmt.Sprintf("0x%x", rand.Uint64())
	n.logs = []map[string]any{transferLog(base.Token, evmWallet, units, tx)}

	// The first read starts at the chain's final block and reads nothing
	// from before it.
	m.tick(ctx)
	if n.gets != 0 {
		t.Fatalf("the first read asked for %d spans of history", n.gets)
	}
	n.head = 200
	m.tick(ctx)
	got, err := ask(t, m.checkout, edgeBird, Ask{User: user.String(), ID: c.ID})
	if err != nil {
		t.Fatal(err)
	}
	paid := got.(Checkout)
	if paid.State != store.InvoicePaid || paid.Paid != t0.UnixMilli() || paid.Until != t0.AddDate(0, 3, 0).UnixMilli() || paid.Pay != "" {
		t.Fatalf("after the payment: %+v", paid)
	}
	st, _ := ask(t, m.supporter, "merlin", Ask{User: user.String()})
	if !st.(Status).Active {
		t.Fatalf("status %+v", st)
	}
	// Read again: the same transfer is already seen and buys nothing more.
	n.head = 300
	m.tick(ctx)
	if s2, _ := m.store.Status(ctx, user, t0); !s2.Until.Equal(t0.AddDate(0, 3, 0)) {
		t.Fatalf("read twice: until %v", s2.Until)
	}
	cs, _ := m.store.Cursors(ctx)
	if h := health(cs, "base:USDC", t0.Add(time.Minute)); h != "✓ 1m ago" {
		t.Fatalf("health %q", h)
	}
}

func TestARailThatFailsKeepsItsCursorAndSaysWhy(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }))
	t.Cleanup(down.Close)
	m := running(t, Wallets{EVM: evmWallet}, down.URL)
	m.tick(context.Background())
	cs, err := m.store.Cursors(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	h := health(cs, "base:USDC", t0)
	if !strings.HasPrefix(h, "✗ ") || !strings.Contains(h, "429") {
		t.Fatalf("health %q", h)
	}
	if h := health(nil, "base:USDC", t0); h != "not read yet" {
		t.Fatalf("never read: %q", h)
	}
}

// solanaNode answers for solanaWallet: a USDC account it already holds,
// whose one transaction after since pays amount; nothing for USDT; the
// wallet's own SOL untouched.
func solanaNode(t *testing.T, amount *big.Int, sig string) *httptest.Server {
	usdc, _ := chain.Find("solana:USDC")
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var result any
		switch req.Method {
		case "getTokenAccountsByOwner":
			var f map[string]string
			_ = json.Unmarshal(req.Params[1], &f)
			value := []any{}
			if f["mint"] == usdc.Token {
				value = append(value, map[string]any{"pubkey": usdcAccount})
			}
			result = map[string]any{"value": value}
		case "getSignaturesForAddress":
			var account string
			_ = json.Unmarshal(req.Params[0], &account)
			result = []any{}
			if account == usdcAccount {
				result = []any{map[string]any{"signature": sig, "err": nil, "blockTime": t0.Unix() + 30}}
			}
		case "getTransaction":
			bal := func(idx int, owner, amount string) map[string]any {
				return map[string]any{"accountIndex": idx, "mint": usdc.Token, "owner": owner, "uiTokenAmount": map[string]any{"amount": amount, "decimals": 6}}
			}
			result = map[string]any{"meta": map[string]any{"err": nil, "preBalances": []int{9, 1}, "postBalances": []int{8, 1},
				"preTokenBalances":  []any{bal(1, solanaWallet, "0")},
				"postTokenBalances": []any{bal(1, solanaWallet, amount.String())}},
				"transaction": map[string]any{"message": map[string]any{"accountKeys": []any{map[string]any{"pubkey": "Payer", "signer": true}, map[string]any{"pubkey": usdcAccount}}}}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
	}))
	t.Cleanup(s.Close)
	return s
}

func TestAPaymentOnSolanaIsSeenInTheTokenAccount(t *testing.T) {
	up := true
	p := prices(t, &up)
	m := running(t, Wallets{Solana: solanaWallet}, p.URL)
	user := someone()
	out, err := ask(t, m.quote, edgeBird, Ask{User: user.String(), Months: 1, Cents: 500, Rail: "solana:USDC"})
	if err != nil {
		t.Fatal(err)
	}
	units, _ := new(big.Int).SetString(out.(Checkout).Units, 10)
	node := solanaNode(t, units, fmt.Sprint("sig", rand.Uint64()))
	m.reader.RPC["solana"] = node.URL
	m.tick(context.Background())
	got, _ := ask(t, m.checkout, edgeBird, Ask{User: user.String(), ID: out.(Checkout).ID})
	if got.(Checkout).State != store.InvoicePaid {
		t.Fatalf("after the payment: %+v", got)
	}
}

func TestAPaymentOnTronIsSeen(t *testing.T) {
	usdt, _ := chain.Find("tron:USDT")
	var amount *big.Int
	tx := fmt.Sprint("t", rand.Uint64())
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := []any{}
		if amount != nil {
			data = append(data, map[string]any{"transaction_id": tx, "token_info": map[string]any{"address": usdt.Token}, "block_timestamp": t0.UnixMilli() + 5, "from": "TSender", "to": tronWallet, "type": "Transfer", "value": amount.String()})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data, "meta": map[string]any{}})
	}))
	t.Cleanup(s.Close)
	m := running(t, Wallets{Tron: tronWallet}, s.URL)
	user := someone()
	out, err := ask(t, m.quote, edgeBird, Ask{User: user.String(), Months: 6, Cents: 500, Rail: "tron:USDT"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	m.tick(ctx) // starts the cursor at now
	amount, _ = new(big.Int).SetString(out.(Checkout).Units, 10)
	m.tick(ctx)
	got, _ := ask(t, m.checkout, edgeBird, Ask{User: user.String(), ID: out.(Checkout).ID})
	if got.(Checkout).State != store.InvoicePaid || got.(Checkout).Until != t0.AddDate(0, 6, 0).UnixMilli() {
		t.Fatalf("after the payment: %+v", got)
	}
}

// buttons is every button's label in the panel /support shows user.
func buttons(t *testing.T, m *Module, user string) []string {
	t.Helper()
	e, sent := coretest.Event(t, "support", func(p map[string]any) {
		p["member"].(map[string]any)["user"] = map[string]any{"id": user, "username": "u"}
	})
	if err := m.open(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, c := range (*sent)[0].Components {
		for _, sub := range c.(discord.ContainerComponent).Components {
			if row, ok := sub.(discord.ActionRowComponent); ok {
				for _, b := range row.Components {
					if btn, ok := b.(discord.ButtonComponent); ok {
						out = append(out, btn.Label)
					}
				}
			}
		}
	}
	return out
}

// panel is /support run by user, as text.
func panel(t *testing.T, m *Module, user string) string {
	t.Helper()
	e, sent := coretest.Event(t, "support", func(p map[string]any) {
		p["member"].(map[string]any)["user"] = map[string]any{"id": user, "username": "u"}
	})
	if err := m.open(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if (*sent)[0].Flags&discord.MessageFlagEphemeral == 0 {
		t.Fatal("the panel isn't ephemeral")
	}
	return coretest.Text("", (*sent)[0].Components)
}

func TestThePanelShowsYourBackingAndTheKeeperTheTakings(t *testing.T) {
	n := &evmNode{head: 100}
	m := running(t, Wallets{EVM: evmWallet}, n.serve(t).URL)
	ctx := context.Background()
	user := someone()
	text := panel(t, m, user.String())
	if !strings.Contains(text, "**backing** not yet") || strings.Contains(text, "supporters") {
		t.Fatalf("someone who doesn't yet:\n%s", text)
	}
	if b := strings.Join(buttons(t, m, user.String()), ","); b != "back the birds" {
		t.Fatalf("someone's buttons: %s", b)
	}
	if b := strings.Join(buttons(t, m, keeper.String()), ","); b != "back the birds,give backing,give gold backing,check again" {
		t.Fatalf("the keeper's buttons: %s", b)
	}
	if _, err := m.store.Gift(ctx, user, 1, true, keeper, t0.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if text := panel(t, m, user.String()); !strings.Contains(text, "**backing** gold, until 25 nov 2026") || !strings.Contains(text, "since 25 oct 2026") {
		t.Fatalf("a gold supporter:\n%s", text)
	}
	if b := strings.Join(buttons(t, m, user.String()), ","); b != "add to it" {
		t.Fatalf("a supporter's buttons: %s", b)
	}
	m.now = func() time.Time { return t0.AddDate(0, 2, 0) }
	if text := panel(t, m, user.String()); !strings.Contains(text, "**backing** ran out 25 nov 2026") {
		t.Fatalf("after it ran out:\n%s", text)
	}
	m.now = func() time.Time { return t0 }
	// Something arrived that paid nothing.
	base, _ := chain.Find("base:USDC")
	if _, err := m.store.Record(ctx, store.Payment{Rail: base.Key, Tx: fmt.Sprintf("0x%x", rand.Uint64()), Amount: big.NewInt(4_500_000), Seen: t0.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.store.Record(ctx, store.Payment{Rail: "dropped:USDC", Tx: fmt.Sprintf("0x%x", rand.Uint64()), Amount: big.NewInt(7), Seen: t0}); err != nil {
		t.Fatal(err)
	}
	keep := panel(t, m, keeper.String())
	if strings.Contains(keep, "%!") {
		t.Fatalf("a payment rendered badly:\n%s", keep)
	}
	for _, want := range []string{"**supporters**", "**30 days**", "usdc on base", "**chains** usdc on base", "4.5 usdc on base matched no checkout, 1h 0m ago: [the transfer](https://basescan.org/tx/0x"} {
		if !strings.Contains(keep, want) {
			t.Errorf("the keeper's panel lacks %q:\n%s", want, keep)
		}
	}
}

func TestOnlyTheKeeperGivesBacking(t *testing.T) {
	m := running(t, Wallets{EVM: evmWallet}, "http://unused")
	ctx := context.Background()
	press := func(user string) (discord.InteractionResponseData, error) {
		e, _ := coretest.Button(t, id+":gift:gold", func(p map[string]any) {
			p["member"].(map[string]any)["user"] = map[string]any{"id": user, "username": "u"}
		})
		var got discord.InteractionResponseData
		e.Respond = func(_ discord.InteractionResponseType, d discord.InteractionResponseData, _ ...rest.RequestOpt) error {
			got = d
			return nil
		}
		return got, m.click(ctx, e)
	}
	if _, err := press("6"); err == nil {
		t.Fatal("someone else opened the gift form")
	}
	form, err := press(keeper.String())
	if err != nil || form.(discord.ModalCreate).CustomID != giftID+":gold" {
		t.Fatalf("the keeper's form: %+v %v", form, err)
	}
	user := someone()
	submit := func(by string, values map[string]string) (string, error) {
		e, _ := coretest.Modal(t, giftID+":gold", values, func(p map[string]any) {
			p["member"].(map[string]any)["user"] = map[string]any{"id": by, "username": "u"}
		})
		var got discord.MessageUpdate
		e.Respond = func(_ discord.InteractionResponseType, d discord.InteractionResponseData, _ ...rest.RequestOpt) error {
			got = d.(discord.MessageUpdate)
			return nil
		}
		if err := m.gifted(ctx, e); err != nil {
			return "", err
		}
		return coretest.Text("", *got.Components), nil
	}
	if _, err := submit("6", map[string]string{"user": user.String(), "months": "3"}); err == nil {
		t.Fatal("someone else gave backing")
	}
	for _, bad := range []map[string]string{{"user": "steve", "months": "3"}, {"user": user.String(), "months": "0"}, {"user": user.String(), "months": "37"}, {"user": user.String(), "months": "x"}} {
		if _, err := submit(keeper.String(), bad); err == nil {
			t.Errorf("%v: given", bad)
		} else if _, ok := errors.AsType[core.Tell](err); !ok {
			t.Errorf("%v: %v isn't said to the keeper", bad, err)
		}
	}
	text, err := submit(keeper.String(), map[string]string{"user": user.String(), "months": "3"})
	if err != nil || !strings.Contains(text, fmt.Sprintf("✓ gave <@%s> 3 months of gold backing, until 25 jan 2027", user)) {
		t.Fatalf("given: %s %v", text, err)
	}
	if st, _ := m.store.Status(ctx, user, t0); !st.Active || !st.Gold {
		t.Fatalf("status %+v", st)
	}
	// A fresh look redraws the panel.
	e, _ := coretest.Button(t, id, nil)
	var redrawn discord.MessageUpdate
	e.Respond = func(_ discord.InteractionResponseType, d discord.InteractionResponseData, _ ...rest.RequestOpt) error {
		redrawn = d.(discord.MessageUpdate)
		return nil
	}
	if err := m.click(ctx, e); err != nil || !strings.Contains(coretest.Text("", *redrawn.Components), "**supporters**") {
		t.Fatalf("check again: %v", err)
	}
}

func TestWithoutADatabaseTheCommandSaysSo(t *testing.T) {
	m := New(nil, nil, Wallets{}, 9, keeper, "", nil, nil)
	e, _ := coretest.Event(t, "support", nil)
	if err := m.open(context.Background(), e); !errors.Is(err, errNoStore) {
		t.Fatalf("open: %v", err)
	}
	b, _ := coretest.Button(t, id, nil)
	if err := m.click(context.Background(), b); !errors.Is(err, errNoStore) {
		t.Fatalf("click: %v", err)
	}
	if _, err := ask(t, m.supporter, "merlin", Ask{User: "5"}); err == nil {
		t.Fatal("a status without a database")
	}
}

func TestTheModuleIsWellFormed(t *testing.T) {
	m := New(nil, nil, Wallets{}, 9, keeper, "", nil, nil)
	r := core.NewRouter(keeper, func(snowflake.ID) (snowflake.ID, bool) { return 0, false }, nil)
	if err := r.Add(m); err != nil {
		t.Fatal(err)
	}
	if m.Name() != "support" || m.Perms() != 0 || m.Help().About == "" || len(m.Verbs()) != 5 {
		t.Fatal("module")
	}
	if months(1) != "1 month" || months(12) != "12 months" || day(t0) != "25 oct 2026" || clip("abcdef", 5) != "ab..." || clip("abc", 5) != "abc" {
		t.Fatal("words")
	}
	if len(New(nil, nil, Wallets{EVM: evmWallet}, 9, 0, "", nil, nil).Offered()) != 2 {
		t.Fatal("offered")
	}
}

func TestAStaleSOLCheckoutOffersNothingToPay(t *testing.T) {
	up := true
	p := prices(t, &up)
	m := running(t, Wallets{Solana: solanaWallet}, p.URL)
	user := someone()
	out, err := ask(t, m.quote, edgeBird, Ask{User: user.String(), Months: 1, Cents: 500, Rail: "solana:SOL"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	at := func(d time.Duration) Checkout {
		m.now = func() time.Time { return t0.Add(d) }
		if err := m.store.Sweep(ctx, m.now(), grace); err != nil {
			t.Fatal(err)
		}
		c, err := ask(t, m.checkout, edgeBird, Ask{User: user.String(), ID: out.(Checkout).ID})
		if err != nil {
			t.Fatal(err)
		}
		return c.(Checkout)
	}
	if c := at(25 * time.Minute); c.Stale || c.Pay == "" {
		t.Fatalf("lapsed inside the slack: %+v", c)
	}
	if c := at(35 * time.Minute); !c.Stale || c.Pay != "" {
		t.Fatalf("past the slack: %+v", c)
	}
}

func TestANewSolanaWalletIsReadOnlyFromNow(t *testing.T) {
	node := solanaNode(t, big.NewInt(1), fmt.Sprint("sig", rand.Uint64()))
	m := running(t, Wallets{Solana: solanaWallet}, node.URL)
	ctx := context.Background()
	rail, _ := chain.Find("solana:SOL")
	if err := m.scan(ctx, rail); err != nil {
		t.Fatal(err)
	}
	m.wallets.Solana = usdcAccount
	m.now = func() time.Time { return t0.Add(time.Hour) }
	if err := m.scan(ctx, rail); err != nil {
		t.Fatal(err)
	}
	c, err := m.store.Cursor(ctx, rail.Key, t0)
	if err != nil || c.At != usdcAccount || !c.Since.Equal(t0.Add(time.Hour)) {
		t.Fatalf("after the wallet changed: %+v %v", c, err)
	}
}

func TestAnEVMReadThatFailsPartWayKeepsWhatItRead(t *testing.T) {
	base, _ := chain.Find("base:USDC")
	head, logs := uint64(5000), 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch req.Method {
		case "eth_blockNumber":
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": fmt.Sprintf("0x%x", head)})
		default:
			logs++
			if logs > 1 {
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": []any{}})
		}
	}))
	t.Cleanup(srv.Close)
	m := running(t, Wallets{EVM: evmWallet}, srv.URL)
	ctx := context.Background()
	if err := m.scan(ctx, base); err != nil { // starts at the head
		t.Fatal(err)
	}
	start, _ := m.store.Cursor(ctx, base.Key, t0)
	head += 3000
	if err := m.scan(ctx, base); err == nil {
		t.Fatal("a read that failed part way reported nothing")
	}
	c, _ := m.store.Cursor(ctx, base.Key, t0)
	from, _ := strconv.ParseUint(start.At, 10, 64)
	if c.At != strconv.FormatUint(from+1000, 10) || c.Err == "" {
		t.Fatalf("cursor %+v, want one span on from %d and the error kept", c, from)
	}
}

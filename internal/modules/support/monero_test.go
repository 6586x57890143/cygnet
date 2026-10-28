package support

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/6586x57890143/cygnet/internal/chain"
	"golang.org/x/crypto/sha3"
)

func xmrAddress(prefix, seed byte) string {
	raw := make([]byte, 69)
	raw[0] = prefix
	for i := 1; i < 65; i++ {
		raw[i] = seed + byte(i)
	}
	h := sha3.NewLegacyKeccak256()
	_, _ = h.Write(raw[:65])
	copy(raw[65:], h.Sum(nil)[:4])
	var out strings.Builder
	for offset := 0; offset < 69; offset += 8 {
		end := min(offset+8, 69)
		size := 11
		if end-offset == 5 {
			size = 7
		}
		n := new(big.Int).SetBytes(raw[offset:end])
		r := new(big.Int)
		var encoded []byte
		for n.Sign() > 0 {
			n.QuoRem(n, big.NewInt(58), r)
			encoded = append(encoded, "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"[r.Int64()])
		}
		for len(encoded) < size {
			encoded = append(encoded, '1')
		}
		for i := len(encoded) - 1; i >= 0; i-- {
			out.WriteByte(encoded[i])
		}
	}
	return out.String()
}

type xmrNode struct {
	kind     string
	receipts []any
	created  int
	next     uint32
	fail     bool
}

func (n *xmrNode) module(t *testing.T) *Module {
	t.Helper()
	primary := xmrAddress(18, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet {
			_, _ = fmt.Fprintf(w, `{"error":[],"result":{"XXMRZUSD":[["200.00","1",%d]]}}`, time.Now().Unix())
			return
		}
		if req.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", `Digest realm="wallet",nonce="test",algorithm=SHA-256,qop="auth"`)
			w.WriteHeader(401)
			return
		}
		if n.fail {
			w.WriteHeader(503)
			return
		}
		var in struct {
			Method string                     `json:"method"`
			Params map[string]json.RawMessage `json:"params"`
		}
		_ = json.NewDecoder(req.Body).Decode(&in)
		var result any
		switch in.Method {
		case "get_wallet_info":
			result = map[string]any{"address": primary, "network_type": "Mainnet", "wallet_type": n.kind, "wallet_block_height": 100, "daemon_block_height": 100}
		case "create_address":
			n.created++
			addresses := []string{}
			indices := []uint32{}
			for range 8 {
				n.next++
				addresses = append(addresses, xmrAddress(42, byte(n.next)))
				indices = append(indices, n.next)
			}
			result = map[string]any{"addresses": addresses, "address_indices": indices}
		case "store":
			result = map[string]any{}
		case "get_transfers":
			result = map[string]any{"in": n.receipts}
		default:
			t.Errorf("unexpected wallet method %s", in.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "jsonrpc": "2.0", "result": result})
	}))
	t.Cleanup(srv.Close)
	m := running(t, Wallets{Monero: primary}, srv.URL)
	wallet, err := chain.NewMonero(srv.URL, primary, "Mainnet", "cygnet", "test")
	if err != nil {
		t.Fatal(err)
	}
	m.reader.Monero = wallet
	m.reader.Kraken = srv.URL
	return m
}

func TestMoneroCheckoutAccumulatesConfirmedReceiptsAndFeedsMerlin(t *testing.T) {
	n := &xmrNode{kind: "Watch only"}
	m := n.module(t)
	ctx := context.Background()
	if _, offered := m.rail("monero:XMR"); offered {
		t.Fatal("unverified wallet was offered")
	}
	if err := m.prepareMonero(ctx); err != nil {
		t.Fatal(err)
	}
	if !m.reader.Monero.Ready(t0) {
		t.Fatal("wallet failed readiness")
	}
	if err := m.prepareMonero(ctx); err != nil || n.created != 1 {
		t.Fatalf("pool wasn't reused: %d %v", n.created, err)
	}
	u := someone()
	out, err := ask(t, m.quote, edgeBird, Ask{User: u.String(), Months: 1, Cents: 500, Rail: "monero:XMR"})
	if err != nil {
		t.Fatal(err)
	}
	c := out.(Checkout)
	if c.To != xmrAddress(42, 1) || c.Amount != "0.025" || !strings.HasPrefix(c.Pay, "monero:") {
		t.Fatalf("checkout %+v", c)
	}
	receipt := func(tx string, amount uint64) map[string]any {
		return map[string]any{"txid": strings.Repeat(tx, 64), "amount": amount, "height": 99, "timestamp": t0.Add(time.Minute).Unix(), "confirmations": 10, "locked": false, "unlock_time": 0, "double_spend_seen": false, "subaddr_index": map[string]uint32{"major": 0, "minor": 1}}
	}
	n.receipts = []any{receipt("a", 10_000_000_000)}
	rail, _ := chain.Find(c.Rail)
	m.now = func() time.Time { return t0.Add(25 * time.Minute) }
	if err := m.scanMonero(ctx, rail); err != nil {
		t.Fatal(err)
	}
	st, err := ask(t, m.supporter, "merlin", Ask{User: u.String()})
	if err != nil || st.(Status).Active {
		t.Fatalf("partial payment granted backing: %+v %v", st, err)
	}
	n.receipts = append(n.receipts, receipt("b", 15_000_000_000))
	if err := m.scanMonero(ctx, rail); err != nil {
		t.Fatal(err)
	}
	if err := m.scanMonero(ctx, rail); err != nil {
		t.Fatal(err)
	}
	st, err = ask(t, m.supporter, "merlin", Ask{User: u.String()})
	if err != nil || !st.(Status).Active {
		t.Fatalf("full receipt missing: %+v %v", st, err)
	}
	// A scanner error leaves the address cursor retryable.
	n.fail = true
	if err := m.scanMonero(ctx, rail); err == nil {
		t.Fatal("ignored wallet outage")
	}
}

func TestMoneroWatchTickRejectsSpendKeysAndRecovers(t *testing.T) {
	n := &xmrNode{kind: "Normal"}
	m := n.module(t)
	m.tick(context.Background())
	if m.reader.Monero.Ready(t0) || len(m.rails()) != 0 {
		t.Fatal("spend-enabled service offered")
	}
	keep, err := m.keeperView(context.Background(), t0)
	if err != nil || !strings.Contains(keep, "xmr on monero") || !strings.Contains(keep, "not synchronized") {
		t.Fatalf("keeper cannot see the failed rail: %s %v", keep, err)
	}
	n.kind = "Watch only"
	m.tick(context.Background())
	if !m.reader.Monero.Ready(t0) || len(m.rails()) != 1 {
		t.Fatal("view-only wallet didn't recover")
	}
	// Invalid wallet addresses fail startup validation.
	if err := (Wallets{Monero: "bad"}).Check(); err == nil {
		t.Fatal("bad Monero wallet accepted")
	}
}

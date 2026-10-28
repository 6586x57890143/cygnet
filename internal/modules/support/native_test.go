package support

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/6586x57890143/cygnet/internal/chain"
)

func TestNativePaymentFlowsFromCheckoutIntoTheSharedSupporterList(t *testing.T) {
	const tx = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const block = "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	head := 1000
	var amount *big.Int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"data":{"amount":"2000.00","currency":"USD"}}`))
			return
		}
		var in struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		_ = json.NewDecoder(req.Body).Decode(&in)
		var result any
		switch in.Method {
		case "eth_chainId":
			result = "0x2105"
		case "eth_getBlockByNumber":
			var number string
			_ = json.Unmarshal(in.Params[0], &number)
			if number == "finalized" {
				result = map[string]any{"number": fmt.Sprintf("0x%x", head)}
			} else {
				result = map[string]any{"number": "0x3e9", "hash": block, "timestamp": fmt.Sprintf("0x%x", t0.Add(time.Minute).Unix())}
			}
		case "eth_getTransactionReceipt":
			result = map[string]any{"transactionHash": tx, "status": "0x1", "blockNumber": "0x3e9", "blockHash": block}
		case "alchemy_getAssetTransfers":
			result = map[string]any{"transfers": []any{map[string]any{
				"blockNum": "0x3e9", "hash": tx, "uniqueId": tx + ":internal:0", "from": evmWallet, "to": evmWallet, "asset": "ETH", "category": "internal",
				"rawContract": map[string]any{"value": fmt.Sprintf("0x%x", amount), "decimal": "0x12", "address": nil},
			}}}
		default:
			t.Errorf("unexpected call %q", in.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
	}))
	defer srv.Close()
	m := running(t, Wallets{EVM: evmWallet}, srv.URL)
	if _, ok := m.rail("base:ETH"); ok {
		t.Fatal("native ETH offered without an indexer")
	}
	m.reader.Native = map[string]string{"base": srv.URL}
	rail, _ := chain.Find("base:ETH")
	ctx := context.Background()
	if err := m.scan(ctx, rail); err != nil {
		t.Fatal(err)
	}
	u := someone()
	quoted, err := ask(t, m.quote, edgeBird, Ask{User: u.String(), Months: 3, Cents: 500, Rail: rail.Key})
	if err != nil {
		t.Fatal(err)
	}
	c := quoted.(Checkout)
	amount, _ = new(big.Int).SetString(c.Units, 10)
	head = 1002
	m.now = func() time.Time { return t0.Add(40 * time.Minute) }
	if err := m.scan(ctx, rail); err != nil {
		t.Fatal(err)
	}
	if err := m.scan(ctx, rail); err != nil {
		t.Fatal(err)
	}
	status, err := ask(t, m.supporter, "merlin", Ask{User: u.String()})
	if err != nil || !status.(Status).Active {
		t.Fatalf("payment didn't activate backing: %+v %v", status, err)
	}
	all, err := ask(t, m.supporters, "merlin", nil)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, b := range all.(map[string][]Backer)["supporters"] {
		if b.User == u.String() {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("supporter appeared %d times", found)
	}
	// A destination already quoted never follows a later config change.
	m.wallets.EVM = "0x2222222222222222222222222222222222222222"
	i, err := m.store.Invoice(ctx, c.ID)
	if err != nil || m.view(i).To != evmWallet {
		t.Fatalf("quoted destination changed: %+v %v", i, err)
	}
}

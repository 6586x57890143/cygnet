package chain

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMoneroQuotesUseFreshExactExchangeTrades(t *testing.T) {
	now := time.Unix(1700000000, 0)
	response := `{"error":[],"result":{"XXMRZUSD":[["217.123456789","1",1700000000.123,"b","m",""]],"last":"cursor"}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/0/public/Trades" || r.URL.Query().Get("pair") != "XMRUSD" || r.URL.Query().Get("count") != "1" {
			t.Error("wrong price request")
		}
		_, _ = fmt.Fprint(w, response)
	}))
	defer srv.Close()
	r := &Reader{Kraken: srv.URL}
	value, err := r.moneroUSD(context.Background(), now)
	if err != nil || value.RatString() != "217123456789/1000000000" {
		t.Fatalf("inexact price: %v %v", value, err)
	}
	for _, body := range []string{
		`not json`, `{"error":["denied"]}`, `{"result":{}}`,
		`{"result":{"XXMRZUSD":[["217"]]}}`,
		`{"result":{"XXMRZUSD":[["NaN","1",1700000000]]}}`,
		`{"result":{"XXMRZUSD":[["0","1",1700000000]]}}`,
		`{"result":{"XXMRZUSD":[["10001","1",1700000000]]}}`,
		`{"result":{"XXMRZUSD":[["217","1",1699999000]]}}`,
		`{"result":{"XXMRZUSD":[["217","1",1700001000]]}}`,
		`{"result":{"XXMRZUSD":[["217","1","bad"]]}}`,
		`{"result":{"XXMRZUSD":[["217","1",1e999999999]]}}`,
		`{"result":{"XXMRZUSD":[["217","1",1e-999999999]]}}`,
		`{"result":{"XXMRZUSD":[["217","1",999999999999999999999]]}}`,
	} {
		response = body
		if _, err := r.moneroUSD(context.Background(), now); err == nil {
			t.Errorf("accepted unsafe trade %s", body)
		}
	}
}

func TestMoneroAmountsAndWalletLinks(t *testing.T) {
	r := rail(t, "monero:XMR")
	price := big.NewRat(200, 1)
	a, err := r.Pick(500, price)
	if err != nil || a.String() != "25000000000" || r.Format(a) != "0.025" {
		t.Fatalf("amount %v %v", a, err)
	}
	if got := r.Pay(moneroAddress(42, 1), a, "back the birds", ""); !strings.Contains(got, "tx_amount=0.025") || !strings.HasPrefix(got, "monero:") {
		t.Fatalf("URI %s", got)
	}
}

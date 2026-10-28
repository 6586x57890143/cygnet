package chain

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/icholy/digest"
	"golang.org/x/crypto/sha3"
)

func moneroAddress(prefix, seed byte) string {
	raw := make([]byte, 69)
	raw[0] = prefix
	for i := 1; i < 65; i++ {
		raw[i] = seed + byte(i)
	}
	h := sha3.NewLegacyKeccak256()
	_, _ = h.Write(raw[:65])
	copy(raw[65:], h.Sum(nil)[:4])
	var out strings.Builder
	for offset := 0; offset < len(raw); offset += 8 {
		end := min(offset+8, len(raw))
		size := 11
		if end-offset == 5 {
			size = 7
		}
		n := new(big.Int).SetBytes(raw[offset:end])
		r := new(big.Int)
		var encoded []byte
		for n.Sign() > 0 {
			n.QuoRem(n, big.NewInt(58), r)
			encoded = append(encoded, base58Alphabet[r.Int64()])
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

func walletNode(t *testing.T, answer func(string, map[string]json.RawMessage) any) *Monero {
	t.Helper()
	challenge := `Digest realm="wallet", nonce="fixed-test-nonce", algorithm=SHA-256, qop="auth"`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", challenge)
			w.WriteHeader(401)
			return
		}
		credentials, err := digest.ParseCredentials(r.Header.Get("Authorization"))
		if err != nil {
			t.Errorf("invalid authentication: %v", err)
			w.WriteHeader(401)
			return
		}
		chal, _ := digest.ParseChallenge(challenge)
		expected, _ := digest.Digest(chal, digest.Options{Username: "cygnet", Password: "test-secret", Method: r.Method, URI: r.URL.RequestURI(), Cnonce: credentials.Cnonce, Count: credentials.Nc})
		if credentials.Username != "cygnet" || credentials.Response != expected.Response {
			t.Error("wrong digest credential")
			w.WriteHeader(401)
			return
		}
		var in struct {
			Method string                     `json:"method"`
			Params map[string]json.RawMessage `json:"params"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			w.WriteHeader(400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": answer(in.Method, in.Params)})
	}))
	t.Cleanup(srv.Close)
	wallet, err := NewMonero(srv.URL, moneroAddress(18, 1), "Mainnet", "cygnet", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	return wallet
}

func TestMoneroAddressesValidateNetworkTypeAndChecksum(t *testing.T) {
	// Published get_wallet_info example, independent of the test encoder.
	address := "45Ps9mEFLZQ5hqrFVMsH7gXvydddfW5EnA185A2oQL69SzSVKTGxXAGebDaWqs2uNSUYmGoQMTLeT9QPxtwfXEh45nSwxz1"
	if network, sub, valid := MoneroAddress(address); !valid || sub || network != "Mainnet" {
		t.Fatal("rejected official primary-address example")
	}
	for _, tc := range []struct {
		prefix  byte
		network string
		sub     bool
	}{{18, "Mainnet", false}, {42, "Mainnet", true}, {24, "Stagenet", false}, {36, "Stagenet", true}} {
		address := moneroAddress(tc.prefix, 1)
		network, sub, valid := MoneroAddress(address)
		if !valid || network != tc.network || sub != tc.sub || !Valid(MoneroFamily, address) {
			t.Fatalf("bad address decode: %s %v %v", network, sub, valid)
		}
		for _, bad := range []string{address[:94], address + "1", address[:94] + "0", address[:94] + map[bool]string{true: "2", false: "1"}[address[94] == '1']} {
			if _, _, ok := MoneroAddress(bad); ok {
				t.Errorf("accepted invalid address %s", bad)
			}
		}
	}
	if _, _, ok := MoneroAddress(moneroAddress(19, 1)); ok {
		t.Fatal("accepted integrated address")
	}
	if _, _, ok := MoneroAddress(strings.Repeat("z", 95)); ok {
		t.Fatal("accepted overflow")
	}
	if _, _, ok := MoneroAddress(strings.Repeat("1", 95)); ok {
		t.Fatal("accepted zero address")
	}
}

func TestMoneroConfigurationStaysPrivateAndPinsTheWallet(t *testing.T) {
	primary := moneroAddress(18, 1)
	for _, endpoint := range []string{"https://public.example/json_rpc", "http://user:pass@127.0.0.1:1", "http://127.0.0.1:1?key=secret", "http://127.0.0.1:1#x", "ftp://127.0.0.1:1", "http://127.0.0.1:1/other", "http://xmr-wallet:18088", "http://192.168.1.2:18088", "://bad"} {
		if _, err := NewMonero(endpoint, primary, "Mainnet", "u", "p"); err == nil {
			t.Errorf("accepted endpoint %s", endpoint)
		}
	}
	for _, tc := range [][3]string{{primary, "", "p"}, {primary, "u", ""}, {moneroAddress(42, 1), "u", "p"}, {"bad", "u", "p"}} {
		if _, err := NewMonero("http://xmr-wallet:18088", tc[0], "Mainnet", tc[1], tc[2]); err == nil {
			t.Fatal("accepted invalid credentials/address")
		}
	}
	if _, err := NewMonero("http://xmr-wallet:18088", primary, "Stagenet", "u", "p"); err == nil {
		t.Fatal("accepted wrong network")
	}
	w, err := NewMonero("https://xmr-wallet:18088", primary, "", "u", "p")
	if err != nil || w.Network != "Mainnet" {
		t.Fatal(err)
	}
}

func TestMoneroReadinessRequiresPinnedAddressAndSuccessfulRefresh(t *testing.T) {
	now := time.Now()
	address := moneroAddress(18, 1)
	height := uint64(100)
	refreshFails := false
	w := walletNode(t, func(method string, _ map[string]json.RawMessage) any {
		switch method {
		case "get_address":
			return map[string]any{"address": address}
		case "refresh":
			if refreshFails {
				return nil
			}
			return map[string]any{"blocks_fetched": 0, "received_money": false}
		case "get_height":
			return map[string]any{"height": height}
		}
		t.Errorf("unexpected method %s", method)
		return nil
	})
	if w.Ready(now) {
		t.Fatal("ready before validation")
	}
	if err := w.Check(context.Background(), now); err != nil || !w.Ready(now.Add(time.Second)) || w.Ready(now.Add(3*time.Minute)) || w.Ready(now.Add(-time.Second)) {
		t.Fatalf("readiness: %v", err)
	}
	for _, mode := range []string{"wrong-address", "zero-height", "refresh-failed"} {
		address, height, refreshFails = moneroAddress(18, 1), 100, false
		switch mode {
		case "wrong-address":
			address = moneroAddress(18, 2)
		case "zero-height":
			height = 0
		case "refresh-failed":
			refreshFails = true
		}
		if err := w.Check(context.Background(), now); err == nil || w.Ready(now) {
			t.Errorf("accepted unhealthy %s", mode)
		}
	}
	address, height, refreshFails = moneroAddress(18, 1), 100, false
	if err := w.Check(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	w.Invalidate()
	if w.Ready(now) {
		t.Fatal("invalidation ignored")
	}
	if err := w.rpc(context.Background(), "sweep_all", nil, nil); err == nil {
		t.Fatal("client exposes spending")
	}
}

func TestMoneroLegacyDigestRequiresVerifiedTLSAndLongPassword(t *testing.T) {
	password := strings.Repeat("disposable-test-", 3)
	challenge := `Digest realm="wallet", nonce="tls-fixture", algorithm=MD5, qop="auth"`
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", challenge)
			w.WriteHeader(401)
			return
		}
		credentials, err := digest.ParseCredentials(r.Header.Get("Authorization"))
		if err != nil {
			t.Error(err)
			w.WriteHeader(401)
			return
		}
		chal, _ := digest.ParseChallenge(challenge)
		expected, _ := digest.Digest(chal, digest.Options{Username: "fixture", Password: password, Method: r.Method, URI: r.URL.RequestURI(), Cnonce: credentials.Cnonce, Count: credentials.Nc})
		if credentials.Response != expected.Response {
			w.WriteHeader(401)
			return
		}
		_, _ = fmt.Fprint(w, `{"id":1,"result":{"height":100}}`)
	}))
	defer srv.Close()
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	w, err := NewMonero(srv.URL, moneroAddress(18, 1), "Mainnet", "fixture", password, certificate)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.rpc(context.Background(), "get_height", map[string]any{}, &struct{}{}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		password string
		roots    [][]byte
	}{{password, nil}, {"short", [][]byte{certificate}}} {
		w, err := NewMonero(srv.URL, moneroAddress(18, 1), "Mainnet", "fixture", tc.password, tc.roots...)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.rpc(context.Background(), "get_height", map[string]any{}, &struct{}{}); err == nil {
			t.Fatal("legacy Digest accepted without verified TLS or a long password")
		}
	}
	for _, roots := range [][][]byte{{[]byte("not PEM")}, {certificate, certificate}} {
		if _, err := NewMonero(srv.URL, moneroAddress(18, 1), "Mainnet", "fixture", password, roots...); err == nil {
			t.Fatal("invalid certificate configuration accepted")
		}
	}
	if _, err := NewMonero("http://127.0.0.1:1", moneroAddress(18, 1), "Mainnet", "fixture", password, certificate); err == nil {
		t.Fatal("TLS trust anchor accepted for plaintext endpoint")
	}
}

func TestMoneroAddressBatchIsPersistedBeforeUse(t *testing.T) {
	stored := false
	addresses := []string{}
	indices := []uint32{}
	for n := range 8 {
		addresses = append(addresses, moneroAddress(42, byte(n)))
		indices = append(indices, uint32(n+1))
	}
	w := walletNode(t, func(method string, _ map[string]json.RawMessage) any {
		switch method {
		case "create_address":
			return map[string]any{"addresses": addresses, "address_indices": indices}
		case "store":
			stored = true
			return map[string]any{}
		default:
			t.Fatal(method)
			return nil
		}
	})
	got, err := w.CreateAddresses(context.Background())
	if err != nil || len(got) != 8 || !stored || got[0].Index != 1 {
		t.Fatalf("allocation: %v %v", got, err)
	}
	indices[1] = 1
	if _, err := w.CreateAddresses(context.Background()); err == nil {
		t.Fatal("duplicate index accepted")
	}
	indices[1] = 2
	addresses[1] = moneroAddress(18, 2)
	if _, err := w.CreateAddresses(context.Background()); err == nil {
		t.Fatal("primary address accepted as checkout")
	}
	addresses = addresses[:1]
	if _, err := w.CreateAddresses(context.Background()); err == nil {
		t.Fatal("short batch accepted")
	}
}

func TestMoneroOnlyReturnsConfirmedUnlockedReceipts(t *testing.T) {
	tx := strings.Repeat("a", 64)
	receipt := map[string]any{"txid": tx, "amount": uint64(4503599627370497), "height": 100, "timestamp": 1700000000, "confirmations": 10, "locked": false, "unlock_time": 0, "double_spend_seen": false, "subaddr_index": map[string]uint32{"major": 0, "minor": 1}}
	result := map[string]any{"in": []any{receipt}}
	w := walletNode(t, func(method string, params map[string]json.RawMessage) any {
		if method != "get_transfers" || string(params["account_index"]) != "0" {
			t.Errorf("invalid watcher call: %s %v", method, params)
		}
		return result
	})
	addresses := map[uint32]string{1: moneroAddress(42, 1)}
	got, err := w.Incoming(context.Background(), addresses)
	if err != nil || len(got) != 1 || got[0].Amount.String() != "4503599627370497" || got[0].To != addresses[1] || got[0].Event != "subaddress:1" {
		t.Fatalf("receipt: %+v %v", got, err)
	}
	for field, bad := range map[string]any{"height": 0, "confirmations": 9, "locked": true, "unlock_time": 10000, "double_spend_seen": true} {
		old := receipt[field]
		receipt[field] = bad
		if got, err := w.Incoming(context.Background(), addresses); err != nil || len(got) != 0 {
			t.Errorf("unsafe receipt %s accepted: %+v %v", field, got, err)
		}
		receipt[field] = old
	}
	delete(receipt, "locked")
	if got, err := w.Incoming(context.Background(), addresses); err != nil || len(got) != 0 {
		t.Fatal("missing lock status accepted")
	}
	receipt["locked"] = false
	for field, bad := range map[string]any{"amount": 0, "timestamp": 0, "txid": "bad", "subaddr_index": map[string]uint32{"major": 1, "minor": 1}} {
		old := receipt[field]
		receipt[field] = bad
		if _, err := w.Incoming(context.Background(), addresses); err == nil {
			t.Errorf("invalid %s accepted", field)
		}
		receipt[field] = old
	}
	result["in"] = []any{receipt, receipt}
	if _, err := w.Incoming(context.Background(), addresses); err == nil {
		t.Fatal("duplicate aggregate accepted")
	}
	if got, err := w.Incoming(context.Background(), nil); err != nil || len(got) != 0 {
		t.Fatal("empty watch failed")
	}
	for n := range 33 {
		addresses[uint32(n+1)] = "test"
	}
	if _, err := w.Incoming(context.Background(), addresses); err == nil {
		t.Fatal("unbounded watch accepted")
	}
}

func TestMoneroRPCRejectsWeakAuthenticationAndRedirects(t *testing.T) {
	for _, mode := range []string{"MD5", "redirect", "bad-json", "rpc-error", "oversized", "no-auth"} {
		t.Run(mode, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode != "MD5" && mode != "redirect" && mode != "no-auth" && r.Header.Get("Authorization") == "" {
					w.Header().Set("WWW-Authenticate", `Digest realm="wallet",nonce="test",algorithm=SHA-256,qop="auth"`)
					w.WriteHeader(401)
					return
				}
				switch mode {
				case "MD5":
					w.Header().Set("WWW-Authenticate", `Digest realm="wallet", nonce="n", algorithm=MD5, qop="auth"`)
					w.WriteHeader(401)
				case "redirect":
					w.Header().Set("Location", "https://public.example/secret")
					w.WriteHeader(302)
				case "bad-json":
					_, _ = fmt.Fprint(w, "not json")
				case "rpc-error":
					_, _ = fmt.Fprint(w, `{"id":1,"error":{"code":-1,"message":"test-secret"}}`)
				case "oversized":
					_, _ = fmt.Fprint(w, strings.Repeat("x", (2<<20)+1))
				case "no-auth":
					_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "result": map[string]any{"address": moneroAddress(18, 1), "network_type": "Mainnet", "wallet_type": "Watch only", "wallet_block_height": 100, "daemon_block_height": 100}})
				}
			}))
			defer srv.Close()
			wallet, err := NewMonero(srv.URL, moneroAddress(18, 1), "Mainnet", "cygnet", "test-secret")
			if err != nil {
				t.Fatal(err)
			}
			err = wallet.Check(context.Background(), time.Now())
			if err == nil || strings.Contains(err.Error(), "test-secret") || strings.Contains(err.Error(), "public.example") {
				t.Fatalf("unsafe error: %v", err)
			}
		})
	}
}

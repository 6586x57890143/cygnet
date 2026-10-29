package chain

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNativeEndpointFileRejectsUnsafeOrAmbiguousConfiguration(t *testing.T) {
	const endpoint = "https://base-mainnet.g.alchemy.com/v2/disposable-fixture"
	path := filepath.Join(t.TempDir(), "endpoints.json")
	for _, raw := range []string{
		"", "{}", "[]", "null", "{", `{"base":`, `{"base":true}`, `{"base":null}`, `{"base":{}}`,
		`{"base":"` + endpoint + `","base":"` + endpoint + `"}`,
		`{"BASE":"` + endpoint + `"}`, `{"unknown":"` + endpoint + `"}`,
		`{"ethereum":"` + endpoint + `"}`, `{"base":"` + endpoint + `"} {}`,
		`{"base":"` + endpoint + `"`, strings.Repeat(" ", 8193),
	} {
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadNativeEndpoints(path); err == nil || strings.Contains(err.Error(), "disposable-fixture") {
			t.Fatalf("unsafe config was accepted or disclosed: %v", err)
		}
	}
	for _, value := range []string{"", strings.Replace(endpoint, "https:", "http:", 1), endpoint + "?key=other", endpoint + "#fragment",
		endpoint + "/getNFTs", endpoint + " ", strings.Replace(endpoint, "alchemy.com", "alchemy.com.example", 1),
		strings.Replace(endpoint, "base-mainnet", "user@base-mainnet", 1), "https://127.0.0.1/v2/key"} {
		raw, _ := json.Marshal(map[string]string{"base": value})
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadNativeEndpoints(path); err == nil {
			t.Fatal("accepted an endpoint outside the allowed mainnet host/path")
		}
	}
	if _, err := LoadNativeEndpoints(path + ".missing"); err == nil {
		t.Fatal("accepted a missing secret file")
	}
	if _, err := LoadNativeEndpoints(t.TempDir()); err == nil {
		t.Fatal("accepted a directory as a secret file")
	}
	raw := `{"base":"` + endpoint + `","ethereum":"https://eth-mainnet.g.alchemy.com/v2/disposable-fixture"}` + "\n"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	values, err := LoadNativeEndpoints(path)
	if err != nil || len(values) != 2 || values["base"] != endpoint {
		t.Fatalf("valid private endpoints: %v", err)
	}
}

func TestNodeClientRefusesRedirectsAndOversizedResponses(t *testing.T) {
	var forwarded atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded.Store(true)
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "redirect") {
			http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
			return
		}
		_, _ = w.Write([]byte(strings.Repeat(" ", maxBody+1)))
	}))
	defer server.Close()
	r := &Reader{}
	if _, err := r.do(context.Background(), http.MethodGet, server.URL+"/redirect/disposable-fixture", nil, nil); err == nil || forwarded.Load() || strings.Contains(err.Error(), "disposable-fixture") {
		t.Fatalf("redirect protection: forwarded=%v err=%v", forwarded.Load(), err)
	}
	if _, err := r.do(context.Background(), http.MethodGet, server.URL, nil, nil); err == nil {
		t.Fatal("accepted an oversized response")
	}
	if _, err := r.do(context.Background(), http.MethodGet, ":/disposable-fixture", nil, nil); err == nil || strings.Contains(err.Error(), "disposable-fixture") {
		t.Fatalf("invalid request leaked its endpoint: %v", err)
	}
	if message := (&rpcError{Code: -1, Message: "disposable-fixture"}).Error(); strings.Contains(message, "disposable-fixture") {
		t.Fatal("RPC errors must not expose provider-supplied messages")
	}
}

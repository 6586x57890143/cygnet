package main

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFileBounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input")
	for _, size := range []int{0, 1, 64, 65} {
		if err := os.WriteFile(path, []byte(strings.Repeat("x", size)), 0600); err != nil {
			t.Fatal(err)
		}
		raw, err := readFile(path, 64)
		if valid := size > 0 && size <= 64; (err == nil) != valid || (valid && len(raw) != size) {
			t.Fatalf("size %d: len=%d, error=%v", size, len(raw), err)
		}
	}
	if _, err := readFile(path+".missing", 64); err == nil {
		t.Fatal("accepted a missing file")
	}
}

func TestRunRequiresCredentialFiles(t *testing.T) {
	t.Setenv("CYGNET_XMR_RPC_PASSWORD_FILE", filepath.Join(t.TempDir(), "missing"))
	if err := run(t.Context()); err == nil || err.Error() != "wallet check: cannot read RPC password file" {
		t.Fatalf("unexpected credential error: %v", err)
	}
	password := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(password, []byte(strings.Repeat("x", 48)), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CYGNET_XMR_RPC_PASSWORD_FILE", password)
	t.Setenv("CYGNET_XMR_RPC_CA_FILE", filepath.Join(t.TempDir(), "missing"))
	if err := run(t.Context()); err == nil || err.Error() != "wallet check: cannot read pinned certificate" {
		t.Fatalf("unexpected certificate error: %v", err)
	}
}

func TestRunChecksIdentityAuthenticationAndRefresh(t *testing.T) {
	const address = "45Ps9mEFLZQ5hqrFVMsH7gXvydddfW5EnA185A2oQL69SzSVKTGxXAGebDaWqs2uNSUYmGoQMTLeT9QPxtwfXEh45nSwxz1"
	for _, scenario := range []string{"ready", "wrong address", "no authentication", "refresh error", "zero height", "invalid certificate", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if scenario != "no authentication" && r.Header.Get("Authorization") == "" {
					w.Header().Set("WWW-Authenticate", `Digest realm="fixture", nonce="fixture-nonce", algorithm=SHA-256, qop="auth"`)
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				var request struct{ Method string }
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					return
				}
				var result any
				switch request.Method {
				case "get_address":
					value := address
					if scenario == "wrong address" {
						value = "another wallet"
					}
					result = map[string]any{"address": value}
				case "refresh":
					if scenario == "refresh error" {
						_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "error": map[string]any{"code": -1}})
						return
					}
					result = map[string]any{}
				case "get_height":
					height := 1
					if scenario == "zero height" {
						height = 0
					}
					result = map[string]any{"height": height}
				default:
					t.Errorf("unexpected RPC method: %s", request.Method)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
			}))
			defer server.Close()
			certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
			if scenario == "invalid certificate" {
				certificate = []byte("invalid certificate")
			}
			for key, contents := range map[string][]byte{"CYGNET_XMR_RPC_PASSWORD_FILE": []byte(strings.Repeat("x", 48) + "\n"), "CYGNET_XMR_RPC_CA_FILE": certificate} {
				path := filepath.Join(t.TempDir(), "input")
				if err := os.WriteFile(path, contents, 0600); err != nil {
					t.Fatal(err)
				}
				t.Setenv(key, path)
			}
			t.Setenv("CYGNET_XMR_RPC", server.URL+"/json_rpc")
			t.Setenv("CYGNET_SUPPORT_MONERO", address)
			t.Setenv("CYGNET_XMR_NETWORK", "Mainnet")
			t.Setenv("CYGNET_XMR_RPC_USER", "fixture")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if scenario == "cancelled" {
				cancel()
			}
			if err := run(ctx); (err == nil) != (scenario == "ready") {
				t.Fatalf("unexpected readiness result: %v", err)
			}
		})
	}
}

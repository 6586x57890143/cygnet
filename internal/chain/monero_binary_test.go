package chain

import (
	"context"
	"os"
	"testing"
	"time"
)

// Opt-in real-release check. The disposable service must have no daemon
// connectivity, no funds, and a view-only stagenet wallet. It is run inside
// the service's network-none namespace by the local runtime smoke harness.
func TestMoneroReleasedWalletRPC(t *testing.T) {
	if os.Getenv("CYGNET_XMR_SMOKE") != "disposable" {
		t.Skip("requires disposable offline wallet image")
	}
	certificate, err := os.ReadFile("/fixture/xmr-rpc-ca.crt")
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewMonero("https://127.0.0.1:18088/json_rpc", os.Getenv("CYGNET_XMR_SMOKE_ADDRESS"), "Stagenet", "fixture", "disposable-test-password-for-tls-authentication", certificate)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var address struct {
		Address string `json:"address"`
	}
	if err := m.rpc(ctx, "get_address", map[string]any{"account_index": 0}, &address); err != nil || address.Address != m.Primary {
		t.Fatalf("real wallet identity: %v", err)
	}
	addresses, err := m.CreateAddresses(ctx)
	if err != nil || len(addresses) != 8 {
		t.Fatalf("real stored subaddresses: %v", err)
	}
	watched := map[uint32]string{}
	for _, a := range addresses {
		watched[a.Index] = a.Address
	}
	if got, err := m.Incoming(ctx, watched); err != nil || len(got) != 0 {
		t.Fatalf("real incoming receipts: %v", err)
	}
	if err := m.Check(ctx, time.Now()); err == nil || m.Ready(time.Now()) {
		t.Fatal("offline daemon must prevent new quotes")
	}
	if err := m.rpc(ctx, "query_key", nil, &struct{}{}); err == nil {
		t.Fatal("client exposed key export")
	}
}

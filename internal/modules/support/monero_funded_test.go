package support

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/chain"
)

// Opt-in public-stagenet test. The driver supplies a disposable watch-only
// wallet, a known local test database and worthless test coins. The real
// receipt reader and PostgreSQL settlement paths are never mocked.
func TestMoneroFundedStagenetSettlementAndRestore(t *testing.T) {
	if os.Getenv("CYGNET_XMR_FUNDED_SMOKE") != "stagenet-faucet" {
		t.Skip("requires an explicitly prepared disposable stagenet fixture")
	}
	const dsn = "postgres://cygnet:cygnet_test_local@cygnet-payment-tests:5432/cygnet?sslmode=disable"
	if os.Getenv("CYGNET_TEST_DATABASE_URL") != dsn {
		t.Fatal("funded smoke requires its dedicated local fixture database")
	}
	primary := os.Getenv("CYGNET_XMR_FUNDED_PRIMARY")
	if network, sub, valid := chain.MoneroAddress(primary); !valid || sub || network != "Stagenet" {
		t.Fatal("funded smoke refuses non-stagenet wallet identities")
	}
	const directory = "/fixture/state"
	write := func(name string, value any) {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(filepath.Join(directory, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal("fixture checkpoint already exists or cannot be created")
		}
		_, err = file.Write(raw)
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			t.Fatal("cannot save fixture checkpoint")
		}
	}
	certificate, err := os.ReadFile("/fixture/xmr-rpc-ca.crt")
	if err != nil {
		t.Fatal("missing disposable pinned certificate")
	}
	wallet, err := chain.NewMonero("https://xmr-wallet:18088/json_rpc", primary, "Stagenet", "fixture", "disposable-test-password-for-tls-authentication", certificate)
	if err != nil {
		t.Fatal(err)
	}
	price := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"error":[],"result":{"XXMRZUSD":[["200.00","1",%d]]}}`, time.Now().Unix())
	}))
	defer price.Close()
	m := running(t, Wallets{Monero: primary}, price.URL)
	m.reader.Monero, m.reader.Kraken = wallet, price.URL
	// The expired fixture is quoted in an earlier logical time, before any
	// driver transaction. Its actual on-chain receipt must stay unpaid.
	past := time.Now().Add(-time.Hour)
	m.now = func() time.Time { return past }
	if err := m.prepareMonero(t.Context()); err != nil {
		t.Fatal(err)
	}
	type fixtureInvoice struct {
		Case, Address, Units string
	}
	type entry struct {
		user     snowflake.ID
		checkout Checkout
	}
	entries := map[string]entry{}
	quote := func(name string) {
		t.Helper()
		user := someone()
		out, err := ask(t, m.quote, edgeBird, Ask{User: user.String(), Months: 1, Cents: 500, Rail: "monero:XMR"})
		if err != nil {
			t.Fatal(err)
		}
		entries[name] = entry{user: user, checkout: out.(Checkout)}
	}
	quote("late")
	m.now = time.Now
	if err := wallet.Check(t.Context(), m.now()); err != nil {
		t.Fatal(err)
	}
	quote("paid")
	quote("partial")
	var invoices []fixtureInvoice
	for _, name := range []string{"paid", "late", "partial"} {
		checkout := entries[name].checkout
		invoices = append(invoices, fixtureInvoice{Case: name, Address: checkout.To, Units: checkout.Units})
	}
	write("invoices.json", invoices)
	t.Log("three dedicated subaddress invoices prepared; waiting for the driver's single test transaction and ten real confirmations")
	status := func(name string) Status {
		t.Helper()
		out, err := ask(t, m.supporter, "merlin", Ask{User: entries[name].user.String()})
		if err != nil {
			t.Fatal(err)
		}
		return out.(Status)
	}
	rail, _ := chain.Find("monero:XMR")
	deadline := time.Now().Add(70 * time.Minute)
	for !status("paid").Active {
		if time.Now().After(deadline) {
			t.Fatal("test receipts did not settle within the stagenet window")
		}
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		err := wallet.Check(ctx, m.now())
		if err == nil {
			err = m.scanMonero(ctx, rail)
		}
		cancel()
		if err != nil {
			t.Log("stagenet scan is retrying without advancing settlement")
		}
		select {
		case <-t.Context().Done():
			t.Fatal("funded fixture cancelled")
		case <-time.After(15 * time.Second):
		}
	}
	paid := status("paid")
	if status("partial").Active || status("late").Active {
		t.Fatal("a partial or late receipt incorrectly granted backing")
	}
	for range 3 {
		if err := m.scanMonero(t.Context(), rail); err != nil {
			t.Fatal(err)
		}
	}
	if status("paid").Until != paid.Until {
		t.Fatal("receipt replay extended a paid period")
	}
	all, err := ask(t, m.supporters, "merlin", nil)
	if err != nil {
		t.Fatal(err)
	}
	backers := all.(map[string][]Backer)["supporters"]
	if len(backers) != 1 || backers[0].User != entries["paid"].user.String() {
		t.Fatal("Merlin's supporter API did not expose exactly the paid fixture user")
	}
	write("settled.json", map[string]any{"until": paid.Until, "late_unpaid": true, "partial_unpaid": true, "replay_safe": true})
	t.Log("real overpayment settled once; late and partial receipts remain unpaid; waiting for the driver to restore the view-only wallet")
	for {
		if _, err := os.Stat(filepath.Join(directory, "restored.json")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("view-only restore checkpoint was not supplied")
		}
		time.Sleep(time.Second)
	}
	if err := wallet.Check(t.Context(), m.now()); err != nil {
		t.Fatal("restored wallet failed identity, authentication or node refresh")
	}
	if err := m.scanMonero(t.Context(), rail); err != nil {
		t.Fatal(err)
	}
	if status("paid").Until != paid.Until || status("late").Active || status("partial").Active {
		t.Fatal("wallet restore changed payment eligibility or duplicated backing")
	}
	write("passed.json", map[string]bool{"funded_settlement": true, "replay": true, "late": true, "partial": true, "view_only_restore": true})
}

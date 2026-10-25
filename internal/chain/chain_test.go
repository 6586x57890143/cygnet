package chain

import (
	"math/big"
	"net/url"
	"strings"
	"testing"
)

func rail(t *testing.T, key string) Rail {
	t.Helper()
	r, ok := Find(key)
	if !ok {
		t.Fatalf("no rail %s", key)
	}
	return r
}

func TestEveryRailIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range Rails {
		if seen[r.Key] || r.Key != r.Chain+":"+r.Asset() || r.Label != strings.ToLower(r.Label) || r.RPC == "" || !strings.Contains(r.Tx, "%s") {
			t.Errorf("rail %+v", r)
		}
		seen[r.Key] = true
		if r.Token != "" && !Valid(r.Family, r.Token) {
			t.Errorf("%s: token %s isn't a %s address", r.Key, r.Token, r.Family)
		}
		if r.Family == EVM && r.Confirmations == 0 {
			t.Errorf("%s counts a transfer before any confirmation", r.Key)
		}
		if r.Decimals < 4 {
			t.Errorf("%s: %d decimals leave no room for change", r.Key, r.Decimals)
		}
	}
	if _, ok := Find("ethereum:USDC"); ok {
		t.Fatal("found a rail cygnet doesn't take")
	}
}

func TestAStablecoinCostsItsPriceWithUnderACentOfChange(t *testing.T) {
	r := rail(t, "base:USDC")
	base, err := r.Base(1400, nil)
	if err != nil || base.Int64() != 14_000_000 {
		t.Fatalf("base %v %v", base, err)
	}
	if r.MaxChange().Int64() != 9999 {
		t.Fatalf("max change %v", r.MaxChange())
	}
	seen := map[int64]bool{}
	for range 200 {
		a, err := r.Pick(1400, nil)
		if err != nil {
			t.Fatal(err)
		}
		c := a.Int64() - 14_000_000
		if c < 1 || c > 9999 {
			t.Fatalf("change %d", c)
		}
		seen[c] = true
	}
	if len(seen) < 150 {
		t.Fatalf("only %d kinds of change in 200 picks", len(seen))
	}
	if _, err := r.Base(0, nil); err == nil {
		t.Fatal("priced nothing")
	}
}

func TestSOLIsQuotedAtItsPriceAndRoundedUp(t *testing.T) {
	r := rail(t, "solana:SOL")
	// $5 at $150 a coin is 0.0333... SOL: up to 0.0334, the next
	// ten-thousandth of a coin, 33400000 lamports.
	usd := big.NewRat(150, 1)
	base, err := r.Base(500, usd)
	if err != nil || base.Int64() != 33_400_000 {
		t.Fatalf("base %v %v", base, err)
	}
	if r.Format(base) != "0.0334" {
		t.Fatalf("format %s", r.Format(base))
	}
	a, err := r.Pick(500, usd)
	if err != nil || a.Int64() <= 33_400_000 || a.Int64() >= 33_400_000+100_000 {
		t.Fatalf("pick %v %v", a, err)
	}
	if _, err := r.Base(500, nil); err == nil {
		t.Fatal("quoted SOL with no price")
	}
	if _, err := r.Base(500, big.NewRat(0, 1)); err == nil {
		t.Fatal("quoted SOL at a price of 0")
	}
	// A price that divides exactly is not rounded past itself.
	exact, _ := r.Base(10000, big.NewRat(100, 1))
	if exact.Int64() != 1_000_000_000 {
		t.Fatalf("exact %v", exact)
	}
}

func TestFormatKeepsEveryDigitThatCounts(t *testing.T) {
	r := rail(t, "base:USDC")
	for in, want := range map[int64]string{5_004_217: "5.004217", 14_000_000: "14", 1: "0.000001", 500_000: "0.5", 10_000_001: "10.000001"} {
		if got := r.Format(big.NewInt(in)); got != want {
			t.Errorf("Format(%d) = %s, want %s", in, got, want)
		}
	}
}

func TestSolanaPayLinksPayTheWalletInWholeCoins(t *testing.T) {
	owner := "mvines9iiHiQTysrwkJjGf2gb9Ex9jXJX8ns3qwf2kN"
	link := rail(t, "solana:USDC").Pay(owner, big.NewInt(5_004_217), "back the birds", "3 months of support")
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "solana" || u.Opaque != owner {
		t.Fatalf("link %s: %v", link, err)
	}
	q := u.Query()
	if q.Get("amount") != "5.004217" || q.Get("spl-token") != "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v" || q.Get("label") != "back the birds" {
		t.Fatalf("query %v", q)
	}
	if strings.Contains(link, "+") {
		t.Fatalf("a space as + reads literally in a wallet: %s", link)
	}
	sol := rail(t, "solana:SOL").Pay(owner, big.NewInt(33_340_017), "l", "m")
	if strings.Contains(sol, "spl-token") || !strings.Contains(sol, "amount=0.033340017") {
		t.Fatalf("sol link %s", sol)
	}
	if rail(t, "base:USDC").Pay("0x0", big.NewInt(1), "l", "m") != "" {
		t.Fatal("a link where there is no standard one")
	}
}

func TestAddressesByFamily(t *testing.T) {
	for _, c := range []struct {
		family  Family
		address string
		ok      bool
	}{
		{EVM, "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", true},
		{EVM, "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913", true},
		{EVM, "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA0291", false},
		{Tron, "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t", true},
		{Tron, "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6u", false},
		{Tron, "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", false},
		{Solana, "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", true},
		{Solana, "11111111111111111111111111111111", true},
		{Solana, "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t", false},
		{Solana, "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt10", false},
		{Family("ton"), "x", false},
	} {
		if Valid(c.family, c.address) != c.ok {
			t.Errorf("Valid(%s, %s) = %v", c.family, c.address, !c.ok)
		}
	}
}

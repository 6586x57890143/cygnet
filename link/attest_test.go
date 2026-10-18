package link

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"
)

const commit = "0123456789abcdef0123456789abcdef01234567"

// An attestation verifies only for its own bird, under its own key, with
// every field as signed, well formed and not from the future, and survives
// the trip through an environment variable.
func TestAttestation(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Unix(1_800_000_000, 0)
	good := Sign(Attestation{Bird: "skua", Commit: commit, Image: "sha256:" + commit + commit[:24], Signed: now.Unix()}, priv)
	if err := good.Verify("skua", pub, now); err != nil {
		t.Fatal(err)
	}
	back, err := DecodeAttestation(good.Encode())
	if err != nil || back.Verify("skua", pub, now) != nil {
		t.Fatalf("round trip: %v", err)
	}
	if _, err := DecodeAttestation("%%%"); err == nil {
		t.Error("garbage decodes")
	}
	tamper := func(f func(*Attestation)) Attestation { a := good; f(&a); return a }
	for name, c := range map[string]struct {
		a    Attestation
		bird string
		key  ed25519.PublicKey
		want error
	}{
		"another bird's name": {good, "merlin", pub, errShape},
		"another key":         {good, "skua", other, errSig},
		"a changed commit":    {tamper(func(a *Attestation) { a.Commit = "f" + commit[1:] }), "skua", pub, errSig},
		"a changed image":     {tamper(func(a *Attestation) { a.Image = "" }), "skua", pub, errSig},
		"a short commit":      {tamper(func(a *Attestation) { a.Commit = commit[:7] }), "skua", pub, errShape},
		"a bad digest":        {tamper(func(a *Attestation) { a.Image = "latest" }), "skua", pub, errShape},
		"no date":             {tamper(func(a *Attestation) { a.Signed = 0 }), "skua", pub, errShape},
		"no signature":        {tamper(func(a *Attestation) { a.Sig = nil }), "skua", pub, errSig},
		"a key of no size":    {good, "skua", nil, errSig},
	} {
		if err := c.a.Verify(c.bird, c.key, now); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := good.Verify("skua", pub, now.Add(-time.Hour)); !errors.Is(err, errFuture) {
		t.Errorf("signed an hour ahead: %v", err)
	}
	if !Attested.AtLeast(Signed) || !Signed.AtLeast(Signed) || Untrusted.AtLeast(Signed) || !Untrusted.AtLeast(Untrusted) {
		t.Error("trust's order")
	}
}

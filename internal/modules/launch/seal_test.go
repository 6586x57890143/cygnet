package launch

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

// skR is RFC 9180's A.1.1 recipient key; workerSealed is a token the
// Worker's seal (edge/src/seal.ts) sealed to its public key, so the two
// sides are held to each other.
const (
	skR          = "4612c550263fc8ad58375df3f557aac531d26850903e55a9f23f21d8534e8ac8"
	workerSealed = "lhTepiN4ArylpXGmF_431b1cDPw-bjP0OjzZtBCkXXodZCeo46kXE8h7QJSR4aw5ZG_95qXAhJ02N-jJZG6rFesspWUs_FZg1uGUOQ"
)

func opener(t *testing.T) *Opener {
	t.Helper()
	raw, _ := hex.DecodeString(skR)
	o, err := NewOpener(base64.RawURLEncoding.EncodeToString(raw))
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func TestOpensWhatTheWorkerSealed(t *testing.T) {
	ct, _ := base64.RawURLEncoding.DecodeString(workerSealed)
	got, err := opener(t).Open(ct)
	if err != nil || got != "a refresh token from discord" {
		t.Fatalf("opened %q: %v", got, err)
	}
	ct[len(ct)-1] ^= 1
	if _, err := opener(t).Open(ct); err == nil {
		t.Fatal("a changed seal opened")
	}
	for _, bad := range []string{"!!", base64.RawURLEncoding.EncodeToString([]byte("short"))} {
		if _, err := NewOpener(bad); err == nil {
			t.Fatalf("a key of %q", bad)
		}
	}
}

func TestSealVerb(t *testing.T) {
	m, _ := running(t)
	ctx := context.Background()
	if err := m.store.Register(ctx, "silcord", guild, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := m.store.Join(ctx, "silcord", 11, "", nil, t0); err != nil {
		t.Fatal(err)
	}
	body := func(sealed string) map[string]any {
		return map[string]any{"launch": "silcord", "user": "11", "sealed": sealed}
	}
	if _, err := ask(t, m, "seal", body(workerSealed)); !denied(err, NoSeal) {
		t.Fatalf("sealing with no key: %v", err)
	}
	m.Seal(opener(t))
	got, err := ask(t, m, "seal", body(workerSealed))
	if err != nil || got["sealed"] != true {
		t.Fatalf("seal: %v %v", got, err)
	}
	if p, _ := ask(t, m, "place", body("")); p["sealed"] != true {
		t.Fatalf("place after a seal: %v", p)
	}
	for _, bad := range []string{"!!", "AAAA"} {
		if _, err := ask(t, m, "seal", body(bad)); err == nil {
			t.Fatalf("a seal of %q", bad)
		}
	}
	if got, err := ask(t, m, "seal", body("")); err != nil || got["sealed"] != false {
		t.Fatalf("never mind: %v %v", got, err)
	}
	if _, err := ask(t, m, "seal", map[string]any{"launch": "silcord", "user": "99", "sealed": ""}); !denied(err, NotOnList) {
		t.Fatalf("never mind for someone not on it: %v", err)
	}
}

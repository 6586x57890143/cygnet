package hub

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/6586x57890143/cygnet/link"
)

const commit = "0123456789abcdef0123456789abcdef01234567"

// hello opens a raw link as name with a and returns the close status (or
// the welcome) the hub answers with.
func hello(t *testing.T, url, name string, a *link.Attestation) (link.Welcome, websocket.StatusCode) {
	t.Helper()
	ws := "ws" + strings.TrimPrefix(url, "http") + "/v1/link"
	conn, _, err := websocket.Dial(context.Background(), ws, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + tokens[name]}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	body, _ := json.Marshal(link.Hello{Build: "test", Attest: a})
	_ = link.Write(context.Background(), conn, link.Frame{V: link.V, Kind: link.KindHello, Body: body})
	f, err := link.Read(context.Background(), conn)
	if err != nil {
		return link.Welcome{}, websocket.CloseStatus(err)
	}
	var w link.Welcome
	_ = json.Unmarshal(f.Body, &w)
	return w, -1
}

// A bird whose release key is pinned links only with an attestation that
// key signed for it; an unpinned bird links on its token, untrusted; and
// every call carries the caller's trust, stamped over whatever it wrote.
func TestAttestedLinks(t *testing.T) {
	h, url := fleet(t)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	_, stranger, _ := ed25519.GenerateKey(rand.Reader)
	h.Pin(map[string]ed25519.PublicKey{"skua": pub})
	now := time.Now()
	good := link.Sign(link.Attestation{Bird: "skua", Commit: commit, Signed: now.Unix()}, priv)

	for name, a := range map[string]*link.Attestation{
		"none":            nil,
		"a stranger's":    new(link.Sign(link.Attestation{Bird: "skua", Commit: commit, Signed: now.Unix()}, stranger)),
		"merlin's":        new(link.Sign(link.Attestation{Bird: "merlin", Commit: commit, Signed: now.Unix()}, priv)),
		"from the future": new(link.Sign(link.Attestation{Bird: "skua", Commit: commit, Signed: now.Add(time.Hour).Unix()}, priv)),
	} {
		if _, status := hello(t, url, "skua", a); status != websocket.StatusPolicyViolation {
			t.Errorf("skua with %s attestation: %v", name, status)
		}
	}
	if w, status := hello(t, url, "skua", &good); status != -1 || w.Trust != link.Signed {
		t.Fatalf("skua signed: %+v %v", w, status)
	}
	if w, status := hello(t, url, "merlin", nil); status != -1 || w.Trust != link.Untrusted {
		t.Fatalf("merlin unpinned: %+v %v", w, status)
	}

	// Through clients: merlin sees skua's trust on the call, and a raw
	// peregrine writing "attested" on its own call is stamped back down.
	got := make(chan link.Trust, 2)
	bird(t, url, "merlin", func(c *link.Client) {
		c.Handle("who", func(_ context.Context, call link.Call) (any, error) { got <- call.Trust; return nil, nil })
	})
	skua := link.New(link.Config{URL: "ws" + strings.TrimPrefix(url, "http") + "/v1/link", Token: tokens["skua"], Build: "test", Attest: &good})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = skua.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	linked(t, h, "merlin", "skua")
	if err := skua.Call(context.Background(), "merlin.who", nil, nil); err != nil {
		t.Fatal(err)
	}
	if tr := <-got; tr != link.Signed {
		t.Errorf("skua's call reached merlin as %q", tr)
	}
	conn := raw(t, url, "peregrine", "test")
	_ = link.Write(context.Background(), conn, link.Frame{V: link.V, ID: "1", Kind: link.KindCall, Trust: link.Attested, Method: "merlin.who"})
	if tr := <-got; tr != link.Untrusted {
		t.Errorf("peregrine's forged trust reached merlin as %q", tr)
	}
	if h.TrustOf(Self) != link.Attested || h.TrustOf("nobody") != link.Untrusted || h.TrustOf("skua") != link.Signed {
		t.Error("TrustOf")
	}
	for _, b := range h.Fleet() {
		if b.Name == "skua" && b.Trust != link.Signed {
			t.Errorf("the fleet shows skua as %q", b.Trust)
		}
	}
}

func TestParseKeys(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	k := base64.StdEncoding.EncodeToString(pub)
	keys, err := ParseKeys(" skua=" + k + ", merlin=" + k)
	if err != nil || len(keys) != 2 {
		t.Fatalf("%v %v", keys, err)
	}
	if keys, err := ParseKeys(""); err != nil || len(keys) != 0 {
		t.Errorf("empty: %v %v", keys, err)
	}
	for _, bad := range []string{"skua", "Skua=" + k, "skua=short", "skua=" + k + ",skua=" + k, "=" + k} {
		if _, err := ParseKeys(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

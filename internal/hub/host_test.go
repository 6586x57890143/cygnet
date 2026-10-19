package hub

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/6586x57890143/cygnet/link"
)

var image = "sha256:" + commit + commit[:24]

type fakeHost struct {
	digests []string
	own     bool
	err     error
}

func (f fakeHost) Images(context.Context, string) ([]string, bool, error) {
	return f.digests, f.own, f.err
}

// Where cygnet can see a signed bird's container, the image decides: the
// signed one makes it attested, any other turns it away. Her own tunnel's
// links and a daemon she can't read leave it signed.
func TestHostAttested(t *testing.T) {
	h, url := fleet(t)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	h.Pin(map[string]ed25519.PublicKey{"skua": pub})
	a := link.Sign(link.Attestation{Bird: "skua", Commit: commit, Image: image, Signed: time.Now().Unix()}, priv)
	for name, c := range map[string]struct {
		host  fakeHost
		trust link.Trust
		code  websocket.StatusCode
	}{
		"the signed image":     {fakeHost{digests: []string{"sha256:other", image}}, link.Attested, -1},
		"another image":        {fakeHost{digests: []string{"sha256:other"}}, "", websocket.StatusPolicyViolation},
		"through the tunnel":   {fakeHost{digests: []string{"sha256:cloudflared"}, own: true}, link.Signed, -1},
		"a daemon that's down": {fakeHost{err: errors.New("down")}, link.Signed, -1},
	} {
		h.Watch(c.host)
		w, code := hello(t, url, "skua", &a)
		if code != c.code || (code == -1 && w.Trust != c.trust) {
			t.Errorf("%s: %+v %v", name, w, code)
		}
	}
	// An attestation with no image is only signed, whatever the host says.
	h.Watch(fakeHost{digests: []string{image}})
	plain := link.Sign(link.Attestation{Bird: "skua", Commit: commit, Signed: time.Now().Unix()}, priv)
	if w, code := hello(t, url, "skua", &plain); code != -1 || w.Trust != link.Signed {
		t.Errorf("no image: %+v %v", w, code)
	}
}

// Docker finds the container on the fleet network by its address and
// reads every digest its image answers to.
func TestDocker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/containers/json":
			_ = json.NewEncoder(w).Encode([]any{
				map[string]any{"ImageID": "sha256:aaa", "Labels": map[string]string{"com.docker.compose.project": "skua"},
					"NetworkSettings": map[string]any{"Networks": map[string]any{"fleet": map[string]any{"IPAddress": "172.22.0.5"}}}},
				map[string]any{"ImageID": "sha256:ccc", "Labels": map[string]string{"com.docker.compose.project": "cygnet"},
					"NetworkSettings": map[string]any{"Networks": map[string]any{"fleet": map[string]any{"IPAddress": "172.22.0.2"}}}},
				map[string]any{"ImageID": "sha256:ddd", "NetworkSettings": map[string]any{"Networks": map[string]any{"default": map[string]any{"IPAddress": "172.22.0.9"}}}},
			})
		case "/images/sha256:aaa/json":
			_ = json.NewEncoder(w).Encode(map[string]any{"RepoDigests": []string{"ghcr.io/x/skua@sha256:bbb", "nodigest"}})
		case "/images/sha256:ccc/json":
			_ = json.NewEncoder(w).Encode(map[string]any{"RepoDigests": []string{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	d := Docker{URL: srv.URL + "/", Client: srv.Client(), Project: "cygnet", Network: "fleet"}
	ctx := context.Background()
	got, own, err := d.Images(ctx, "172.22.0.5")
	if err != nil || own || len(got) != 2 || got[0] != "sha256:aaa" || got[1] != "sha256:bbb" {
		t.Errorf("skua's container: %v %v %v", got, own, err)
	}
	if _, own, err := d.Images(ctx, "172.22.0.2"); err != nil || !own {
		t.Errorf("cygnet's tunnel: %v %v", own, err)
	}
	for _, ip := range []string{"172.22.0.9", "10.0.0.1", ""} {
		if _, _, err := d.Images(ctx, ip); !errors.Is(err, errNoContainer) {
			t.Errorf("%q: %v", ip, err)
		}
	}
	bad := Docker{URL: srv.URL + "/nope", Client: srv.Client(), Network: "fleet"}
	if _, _, err := bad.Images(ctx, "172.22.0.5"); err == nil {
		t.Error("a 404 read")
	}
}

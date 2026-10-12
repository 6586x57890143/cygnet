package launch

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/hub"
	"github.com/6586x57890143/cygnet/link"
)

type fleet struct {
	birds   []hub.Bird
	answers map[string]link.Manifest
}

func (f *fleet) Fleet() []hub.Bird { return f.birds }

func (f *fleet) Call(_ context.Context, method string, _, out any) error {
	mf, ok := f.answers[strings.TrimSuffix(method, "."+link.ManifestMethod)]
	if !ok {
		return link.Err(link.Offline)
	}
	*out.(*link.Manifest) = mf
	return nil
}

// users is Discord's bot users: an avatar hash each, or none.
type users map[snowflake.ID]string

func (u users) GetUser(id snowflake.ID, _ ...rest.RequestOpt) (*discord.User, error) {
	hash, ok := u[id]
	if !ok {
		return nil, errors.New("unknown user")
	}
	if hash == "" {
		return &discord.User{ID: id}, nil
	}
	return &discord.User{ID: id, Avatar: &hash}, nil
}

// sky is one server playing both Discord's CDN, an avatar per path, and
// cygnet.lol's admin API, keeping each bird it's sent.
type sky struct {
	mu   sync.Mutex
	got  map[string][]byte
	puts int
}

func (k *sky) serve(t *testing.T) *httptest.Server {
	k.got = map[string][]byte{}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if name, ok := strings.CutPrefix(r.URL.Path, "/admin/bird/"); ok {
			var b struct{ PNG string }
			_ = json.NewDecoder(r.Body).Decode(&b)
			pic, _ := base64.StdEncoding.DecodeString(b.PNG)
			k.mu.Lock()
			k.got[name], k.puts = pic, k.puts+1
			k.mu.Unlock()
			_, _ = w.Write([]byte("{}"))
			return
		}
		switch {
		case strings.Contains(r.URL.Path, "/bad.png"):
			_, _ = w.Write([]byte("not a png"))
		case strings.Contains(r.URL.Path, "/gone.png"):
			w.WriteHeader(http.StatusNotFound)
		case r.URL.Query().Get("size") == "128":
			// A 128 px avatar: its top left quarter red, the rest blue, with
			// one stray pixel in every cell that the majority outvotes.
			img := image.NewNRGBA(image.Rect(0, 0, 128, 128))
			for y := range 128 {
				for x := range 128 {
					c := color.NRGBA{0, 0, 255, 255}
					if x < 64 && y < 64 {
						c = color.NRGBA{255, 0, 0, 255}
					}
					if x%4 == 0 && y%4 == 0 {
						c = color.NRGBA{0, 255, 0, 255}
					}
					img.SetNRGBA(x, y, c)
				}
			}
			_ = png.Encode(w, img)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func TestFlock(t *testing.T) {
	k := &sky{}
	s := k.serve(t)
	f := &fleet{
		birds:   []hub.Bird{{Name: "skua"}, {Name: "merlin"}, {Name: "kite"}, {Name: "wren"}},
		answers: map[string]link.Manifest{"skua": {App: "7"}, "merlin": {App: "8"}, "wren": {App: "x"}},
	}
	m := module(&Edge{URL: s.URL, Token: "secret"}).Flock(f)
	m.flock.cdn = s.URL
	u := users{2: "a", 7: "b", 8: ""}
	now := time.Now()

	// cygnet and skua are sent; merlin has no avatar, kite doesn't answer
	// and wren's manifest names no app, so each is tried again next time.
	m.send(context.Background(), u, 2, now)
	if len(k.got) != 2 || k.got["cygnet"] == nil || k.got["skua"] == nil {
		t.Fatalf("sent %v", keys(k.got))
	}
	pic, err := png.Decode(bytes.NewReader(k.got["skua"]))
	if err != nil {
		t.Fatal(err)
	}
	if b := pic.Bounds(); b.Dx() != cells || b.Dy() != cells {
		t.Fatalf("%v", b)
	}
	if red, blue := color.NRGBAModel.Convert(pic.At(0, 0)), color.NRGBAModel.Convert(pic.At(31, 31)); red != (color.NRGBA{255, 0, 0, 255}) || blue != (color.NRGBA{0, 0, 255, 255}) {
		t.Fatalf("cells %v %v: the stray pixels won", red, blue)
	}

	// Within the hour nothing is sent again; after it, everything is.
	m.send(context.Background(), u, 2, now.Add(time.Minute))
	if k.puts != 2 {
		t.Fatalf("%d puts within the hour", k.puts)
	}
	m.send(context.Background(), u, 2, now.Add(fresh))
	if k.puts != 4 {
		t.Fatalf("%d puts after it", k.puts)
	}

	// An avatar Discord won't give, or that isn't a PNG, is an error.
	for _, path := range []string{"/gone.png", "/bad.png", "://"} {
		if _, err := m.avatar(context.Background(), s.URL+path); err == nil {
			t.Errorf("%s: no error", path)
		}
	}
	if _, err := m.avatar(context.Background(), "http://127.0.0.1:1/x.png"); err == nil {
		t.Error("an unreachable cdn")
	}
}

func TestFly(t *testing.T) {
	// Without the Worker or the hub there's nothing to fly to.
	module(nil).fly(users{}, 2)
	module(&Edge{URL: "http://x"}).fly(users{}, 2)

	k := &sky{}
	s := k.serve(t)
	m := module(&Edge{URL: s.URL, Token: "secret"}).Flock(nil)
	m.flock.cdn = s.URL
	// It starts once: Ready after it doesn't start another.
	m.fly(users{2: "a"}, 2)
	m.OnEvent(&events.Ready{GenericEvent: events.NewGenericEvent(&bot.Client{}, 0, 0)})
	for range 100 {
		k.mu.Lock()
		n := k.puts
		k.mu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("nothing flew")
}

func keys(m map[string][]byte) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

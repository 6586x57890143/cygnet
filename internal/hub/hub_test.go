package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/6586x57890143/cygnet/link"
)

var tokens = map[string]string{
	"skua":      strings.Repeat("s", 64),
	"merlin":    strings.Repeat("m", 64),
	"peregrine": strings.Repeat("p", 64),
}

// fleet is a hub on a test server, and the URL birds link to.
func fleet(t testing.TB) (*Hub, string) {
	t.Helper()
	h := New(tokens, slog.New(slog.DiscardHandler))
	srv := httptest.NewServer(h)
	t.Cleanup(func() { h.Close(); srv.Close() })
	return h, srv.URL
}

// bird links a client as name, set up by setup, and waits until it's linked.
func bird(t testing.TB, url, name string, setup func(*link.Client)) *link.Client {
	t.Helper()
	c := link.New(link.Config{URL: "ws" + strings.TrimPrefix(url, "http") + "/v1/link", Token: tokens[name], Build: "test", Inflight: 2})
	if setup != nil {
		setup(c)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = c.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	for deadline := time.Now().Add(5 * time.Second); c.Name() != name; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("%s never linked", name)
		}
	}
	return c
}

// linked waits until the hub has exactly names linked.
func linked(t testing.TB, h *Hub, names ...string) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
		var got []string
		for _, b := range h.Fleet() {
			got = append(got, b.Name)
		}
		if strings.Join(got, ",") == strings.Join(names, ",") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("linked %v, want %v", got, names)
		}
	}
}

func code(t *testing.T, err error, want link.Code) {
	t.Helper()
	var le *link.Error
	if !errors.As(err, &le) || le.Code != want {
		t.Fatalf("got %v, want %s", err, want)
	}
}

func TestParse(t *testing.T) {
	ok, err := Parse("skua=" + tokens["skua"] + ", merlin=" + tokens["merlin"])
	if err != nil || ok["merlin"] != tokens["merlin"] || len(ok) != 2 {
		t.Fatalf("got %v, %v", ok, err)
	}
	long := strings.Repeat("x", 32)
	for _, bad := range []string{"", "skua", "Skua=" + long, "cygnet=" + long, "skua=short", "skua=" + long + ",skua=" + long} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) accepted", bad)
		}
	}
}

// A call reaches its callee with From stamped by the hub, and the reply
// comes back with the callee's answer.
func TestCallAndReply(t *testing.T) {
	h, url := fleet(t)
	bird(t, url, "merlin", func(c *link.Client) {
		c.Handle("echo", func(_ context.Context, call link.Call) (any, error) {
			var in struct{ Say string }
			if err := call.Bind(&in); err != nil {
				return nil, err
			}
			return map[string]string{"said": in.Say, "from": call.From}, nil
		})
	})
	skua := bird(t, url, "skua", nil)
	linked(t, h, "merlin", "skua")

	var out map[string]string
	if err := skua.Call(context.Background(), "merlin.echo", map[string]string{"say": "hi"}, &out); err != nil {
		t.Fatal(err)
	}
	if out["said"] != "hi" || out["from"] != "skua" {
		t.Fatalf("got %v", out)
	}
	if err := skua.Call(context.Background(), "merlin.echo", "not an object", nil); !errors.Is(err, link.Err(link.Invalid)) {
		t.Fatalf("a bad body: %v", err)
	}
}

func TestRefusals(t *testing.T) {
	h, url := fleet(t)
	release := make(chan struct{})
	bird(t, url, "merlin", func(c *link.Client) {
		// hold answers only once released: answering when its deadline
		// passes could reach the caller before its own timeout does.
		c.Handle("hold", func(context.Context, link.Call) (any, error) {
			<-release
			return nil, nil
		})
		c.Handle("deny", func(context.Context, link.Call) (any, error) { return nil, link.Err(link.Denied, "not for you") })
		c.Handle("boom", func(context.Context, link.Call) (any, error) { return nil, errors.New("secret detail") })
	})
	skua := bird(t, url, "skua", nil)
	linked(t, h, "merlin", "skua")
	ctx := context.Background()

	code(t, skua.Call(ctx, "peregrine.ping", nil, nil), link.Offline)
	code(t, skua.Call(ctx, "merlin.nope", nil, nil), link.Invalid)
	code(t, skua.Call(ctx, "nomethod", nil, nil), link.Invalid)
	code(t, skua.Call(ctx, "merlin.deny", nil, nil), link.Denied)
	err := skua.Call(ctx, "merlin.boom", nil, nil)
	code(t, err, link.Internal)
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("a handler's error leaked: %v", err)
	}

	short, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	code(t, skua.Call(short, "merlin.hold", nil, nil), link.Timeout)
	// The caller can give up a moment before the hub's own timer at the same
	// deadline frees merlin's slot.
	for deadline := time.Now().Add(5 * time.Second); h.Fleet()[0].Inflight != 0; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the timed out call never freed its slot")
		}
	}

	// merlin takes two at once (Inflight: 2); the third is refused.
	errs := make(chan error, 2)
	for range 2 {
		go func() { errs <- skua.Call(ctx, "merlin.hold", nil, nil) }()
	}
	for deadline := time.Now().Add(5 * time.Second); h.Fleet()[0].Inflight < 2; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the two calls never arrived")
		}
	}
	code(t, skua.Call(ctx, "merlin.hold", nil, nil), link.Busy)
	close(release)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}

// Only the bots that subscribed get an event, stamped with who sent it.
func TestEvents(t *testing.T) {
	h, url := fleet(t)
	got := make(chan link.Event, 1)
	var stray atomic.Int32
	bird(t, url, "merlin", func(c *link.Client) { c.On("skua.purge.done", func(e link.Event) { got <- e }) })
	bird(t, url, "peregrine", func(c *link.Client) { c.On("merlin.other", func(link.Event) { stray.Add(1) }) })
	skua := bird(t, url, "skua", func(c *link.Client) { c.On("skua.purge.done", func(link.Event) { stray.Add(1) }) })
	linked(t, h, "merlin", "peregrine", "skua")

	if err := skua.Emit(context.Background(), "purge.done", map[string]int{"n": 3}); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-got:
		if e.From != "skua" || e.Method != "skua.purge.done" || string(e.Body) != `{"n":3}` {
			t.Fatalf("got %+v", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("merlin never heard")
	}
	time.Sleep(50 * time.Millisecond)
	if stray.Load() != 0 {
		t.Fatal("an event reached a bot that didn't subscribe, or its sender")
	}
}

func TestBuiltinsAndFleet(t *testing.T) {
	h, url := fleet(t)
	skua := bird(t, url, "skua", nil)
	linked(t, h, "skua")
	if err := skua.Call(context.Background(), "cygnet.ping", nil, nil); err != nil {
		t.Fatal(err)
	}
	var out struct{ Linked []string }
	if err := skua.Call(context.Background(), "cygnet.fleet", nil, &out); err != nil || len(out.Linked) != 1 || out.Linked[0] != "skua" {
		t.Fatalf("fleet: %v, %v", out, err)
	}
	code(t, skua.Call(context.Background(), "cygnet.nope", nil, nil), link.Invalid)
	if b := h.Fleet()[0]; b.Build != "test" || b.Since.IsZero() {
		t.Fatalf("fleet entry %+v", b)
	}
}

func TestAuthAndHello(t *testing.T) {
	_, url := fleet(t)
	for _, auth := range []string{"", "Bearer ", "Bearer " + strings.Repeat("z", 64), tokens["skua"]} {
		req, _ := http.NewRequest(http.MethodGet, url+"/v1/link", nil)
		req.Header.Set("Authorization", auth)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("auth %q: %d", auth, res.StatusCode)
		}
	}
	req, _ := http.NewRequest(http.MethodGet, url+"/v1/elsewhere", nil)
	req.Header.Set("Authorization", "Bearer "+tokens["skua"])
	if res, err := http.DefaultClient.Do(req); err != nil || res.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown path: %v %v", res, err)
	}

	// A hello in another protocol version is refused at the door.
	ws := "ws" + strings.TrimPrefix(url, "http") + "/v1/link"
	conn, _, err := websocket.Dial(context.Background(), ws, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + tokens["skua"]}}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.CloseNow() }()
	if err := link.Write(context.Background(), conn, link.Frame{V: 99, Kind: link.KindHello}); err != nil {
		t.Fatal(err)
	}
	if _, err := link.Read(context.Background(), conn); websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("v99 hello: %v", err)
	}
}

// A bot can't put words in another's mouth: From is overwritten, and a
// reply to a call it never got goes nowhere.
func TestForgery(t *testing.T) {
	h, url := fleet(t)
	from := make(chan string, 1)
	bird(t, url, "merlin", func(c *link.Client) {
		c.Handle("who", func(_ context.Context, call link.Call) (any, error) { from <- call.From; return nil, nil })
	})
	linked(t, h, "merlin")
	ws := "ws" + strings.TrimPrefix(url, "http") + "/v1/link"
	conn, _, err := websocket.Dial(context.Background(), ws, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + tokens["skua"]}}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.CloseNow() }()
	ctx := context.Background()
	hello, _ := json.Marshal(link.Hello{})
	_ = link.Write(ctx, conn, link.Frame{V: link.V, Kind: link.KindHello, Body: hello})
	if f, err := link.Read(ctx, conn); err != nil || f.Kind != link.KindWelcome {
		t.Fatalf("welcome: %+v %v", f, err)
	}
	_ = link.Write(ctx, conn, link.Frame{V: link.V, ID: "1", Kind: link.KindReply, To: "merlin"})
	_ = link.Write(ctx, conn, link.Frame{V: link.V, ID: "2", Kind: link.KindCall, From: "peregrine", Method: "merlin.who"})
	if got := <-from; got != "skua" {
		t.Fatalf("From reached merlin as %q", got)
	}
	f, err := link.Read(ctx, conn)
	if err != nil || f.Kind != link.KindReply || f.ID != "2" || f.From != "merlin" {
		t.Fatalf("reply %+v %v", f, err)
	}
	// A call with no ID is invalid.
	_ = link.Write(ctx, conn, link.Frame{V: link.V, Kind: link.KindCall, Method: "merlin.who"})
	if f, _ := link.Read(ctx, conn); f.Err == nil || f.Err.Code != link.Invalid {
		t.Fatalf("no id: %+v", f)
	}
}

// Calls over plain HTTP, for a script or a bot without a link.
func TestHTTPCall(t *testing.T) {
	h, url := fleet(t)
	bird(t, url, "merlin", func(c *link.Client) {
		c.Handle("who", func(_ context.Context, call link.Call) (any, error) { return call.From, nil })
	})
	linked(t, h, "merlin")
	post := func(body string) link.Frame {
		req, _ := http.NewRequest(http.MethodPost, url+"/v1/call", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tokens["peregrine"])
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		var f link.Frame
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(res.Body)
		_ = json.Unmarshal(buf.Bytes(), &f)
		return f
	}
	if f := post(`{"method":"merlin.who"}`); f.Err != nil || string(f.Body) != `"peregrine"` || f.Kind != link.KindReply || f.ID == "" {
		t.Fatalf("got %+v", f)
	}
	if f := post(`{"method":"skua.who"}`); f.Err == nil || f.Err.Code != link.Offline {
		t.Fatalf("offline: %+v", f)
	}
	if f := post(`not json`); f.Kind != "" {
		t.Fatalf("bad frame answered %+v", f)
	}
}

// A callee that goes mid-call fails the call offline at once, a newer link
// replaces an older one, and Close says goaway to everyone.
func TestLeavingAndReplacing(t *testing.T) {
	h, url := fleet(t)
	ctx, cancel := context.WithCancel(context.Background())
	merlin := link.New(link.Config{URL: "ws" + strings.TrimPrefix(url, "http") + "/v1/link", Token: tokens["merlin"]})
	// The handler never answers before merlin is gone: an answer racing the
	// closing link would reach skua instead of offline.
	entered, hold := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(hold) })
	merlin.Handle("hang", func(context.Context, link.Call) (any, error) {
		close(entered)
		<-hold
		return nil, nil
	})
	done := make(chan struct{})
	go func() { _ = merlin.Run(ctx); close(done) }()
	skua := bird(t, url, "skua", nil)
	linked(t, h, "merlin", "skua")

	errc := make(chan error, 1)
	go func() { errc <- skua.Call(context.Background(), "merlin.hang", nil, nil) }()
	<-entered
	cancel()
	<-done
	code(t, <-errc, link.Offline)
	linked(t, h, "skua")

	// A second peregrine replaces the first, whose link is closed.
	first := raw(t, url, "peregrine", "first")
	conn := raw(t, url, "peregrine", "second")
	if _, err := link.Read(context.Background(), first); websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("the older link: %v", err)
	}
	if b := h.Fleet()[0]; b.Name != "peregrine" || b.Build != "second" {
		t.Fatalf("fleet %+v", h.Fleet())
	}
	_ = link.Write(context.Background(), conn, link.Frame{V: link.V, Kind: link.KindGoaway})
	for deadline := time.Now().Add(5 * time.Second); !h.Fleet()[0].Draining; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("goaway never marked it draining")
		}
	}
	code(t, skua.Call(context.Background(), "peregrine.anything", nil, nil), link.Busy)

	h.Close()
	if f, err := link.Read(context.Background(), conn); err != nil || f.Kind != link.KindGoaway {
		t.Fatalf("Close: %+v %v", f, err)
	}
	for deadline := time.Now().Add(5 * time.Second); skua.Linked(); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("skua still reads as linked after the hub went away")
		}
	}
}

// raw is a hand-driven link as name, past its welcome.
func raw(t *testing.T, url, name, build string) *websocket.Conn {
	t.Helper()
	ws := "ws" + strings.TrimPrefix(url, "http") + "/v1/link"
	conn, _, err := websocket.Dial(context.Background(), ws, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + tokens[name]}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	hello, _ := json.Marshal(link.Hello{Build: build})
	_ = link.Write(context.Background(), conn, link.Frame{V: link.V, Kind: link.KindHello, Body: hello})
	if f, err := link.Read(context.Background(), conn); err != nil || f.Kind != link.KindWelcome {
		t.Fatalf("welcome: %+v %v", f, err)
	}
	return conn
}

// BenchmarkCall is one call and its reply through the hub, both birds on
// loopback: the floor every bot-to-bot call pays.
func BenchmarkCall(b *testing.B) {
	h, url := fleet(b)
	bird(b, url, "merlin", func(c *link.Client) {
		c.Handle("ping", func(context.Context, link.Call) (any, error) { return nil, nil })
	})
	skua := bird(b, url, "skua", nil)
	linked(b, h, "merlin", "skua")
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if err := skua.Call(ctx, "merlin.ping", nil, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// A manifest crosses the hub intact: what /provision will read.
func TestManifest(t *testing.T) {
	h, url := fleet(t)
	want := link.Manifest{Build: "abc", Perms: "2048", Intents: 1, Commands: []link.Command{{Name: "status", Tier: link.Admin}}}
	bird(t, url, "skua", func(c *link.Client) {
		c.Handle(link.ManifestMethod, func(context.Context, link.Call) (any, error) { return want, nil })
	})
	merlin := bird(t, url, "merlin", nil)
	linked(t, h, "merlin", "skua")
	var got link.Manifest
	if err := merlin.Call(context.Background(), "skua."+link.ManifestMethod, nil, &got); err != nil {
		t.Fatal(err)
	}
	if got.Build != want.Build || got.Perms != want.Perms || got.Intents != 1 || len(got.Commands) != 1 || got.Commands[0] != want.Commands[0] {
		t.Fatalf("got %+v", got)
	}
	if !merlin.Linked() {
		t.Fatal("a linked client reads as unlinked")
	}
}

// cygnet calls a bird herself and gets its answer, or why not.
func TestHubCall(t *testing.T) {
	h, url := fleet(t)
	bird(t, url, "skua", func(c *link.Client) {
		c.Handle(link.ManifestMethod, func(context.Context, link.Call) (any, error) {
			return link.Manifest{Build: "abc", Perms: "1024"}, nil
		})
	})
	linked(t, h, "skua")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var m link.Manifest
	if err := h.Call(ctx, "skua."+link.ManifestMethod, nil, &m); err != nil || m.Build != "abc" || m.Perms != "1024" {
		t.Fatalf("got %+v, %v", m, err)
	}
	code(t, h.Call(ctx, "merlin."+link.ManifestMethod, nil, nil), link.Offline)
	code(t, h.Call(ctx, "skua.nope", nil, nil), link.Invalid)
	if h.Call(ctx, "skua.manifest", func() {}, nil) == nil {
		t.Fatal("a body that will not marshal went out")
	}
}

// Package hub is cygnet's side of the bird protocol (SPEC.md): it knows each
// bot by its token, keeps one link per bot, routes calls by method and
// replies by ID, and hands events to the bots that asked for them.
//
// cygnet authenticates, the callee authorizes: every frame's From is stamped
// here from the token, never taken from the bot. Nothing is queued or
// redelivered; a call either reaches a linked bot before its deadline or
// fails with a code at once.
package hub

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/6586x57890143/cygnet/link"
)

// Self is the name calls to cygnet itself go to: "cygnet.ping".
const Self = "cygnet"

const (
	// maxDeadline caps how far ahead a call may set its deadline.
	maxDeadline = 30 * time.Second
	// helloWithin is how long a new link has to say hello.
	helloWithin = 10 * time.Second
	// pingEvery is the heartbeat; a link that misses one is closed.
	pingEvery = 15 * time.Second
	// writeWithin bounds one frame's write to a slow peer.
	writeWithin = 5 * time.Second
	readLimit   = 1 << 20
	minToken    = 32
)

// Hub is safe for concurrent use. Make one with New.
type Hub struct {
	log    *slog.Logger
	tokens map[[32]byte]string

	mu      sync.Mutex
	peers   map[string]*peer
	pending map[key]*pend
	closed  bool
}

type peer struct {
	name     string
	conn     *websocket.Conn
	hello    link.Hello
	since    time.Time
	inflight atomic.Int32
	draining atomic.Bool
	rtt      atomic.Int64
}

// key is a call in flight: its caller and the caller's ID for it.
type key struct{ caller, id string }

type pend struct {
	callee  *peer
	timer   *time.Timer
	deliver func(link.Frame)
	// fail answers the caller with err, as a reply to its call.
	fail func(*link.Error)
}

// Bird is one linked bot, for /status.
type Bird struct {
	Name, Build string
	Since       time.Time
	RTT         time.Duration
	Inflight    int
	Draining    bool
}

// Parse reads CYGNET_BOTS: "name=token,name=token". Names are lowercase
// letters and digits, cygnet's own is reserved, and a token is at least 32
// characters (openssl rand -hex 32).
func Parse(s string) (map[string]string, error) {
	bots := map[string]string{}
	for pair := range strings.SplitSeq(s, ",") {
		name, token, ok := strings.Cut(strings.TrimSpace(pair), "=")
		switch {
		case !ok || name == "" || strings.Trim(name, "abcdefghijklmnopqrstuvwxyz0123456789") != "":
			return nil, fmt.Errorf("CYGNET_BOTS: %q is not name=token with a lowercase name", name)
		case name == Self:
			return nil, fmt.Errorf("CYGNET_BOTS: %q is cygnet's own name", name)
		case len(token) < minToken:
			return nil, fmt.Errorf("CYGNET_BOTS: %s's token is under %d characters", name, minToken)
		case bots[name] != "":
			return nil, fmt.Errorf("CYGNET_BOTS: %s is listed twice", name)
		}
		bots[name] = token
	}
	return bots, nil
}

// New makes a hub for bots (name to token).
func New(bots map[string]string, log *slog.Logger) *Hub {
	h := &Hub{log: log, tokens: map[[32]byte]string{}, peers: map[string]*peer{}, pending: map[key]*pend{}}
	for name, token := range bots {
		h.tokens[sha256.Sum256([]byte(token))] = name
	}
	return h
}

// ServeHTTP serves GET /v1/link (the WebSocket) and POST /v1/call.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name, ok := h.auth(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch {
	case r.URL.Path == "/v1/link" && r.Method == http.MethodGet:
		h.serveLink(w, r, name)
	case r.URL.Path == "/v1/call" && r.Method == http.MethodPost:
		h.serveCall(w, r, name)
	default:
		http.NotFound(w, r)
	}
}

// auth is the bot a request's bearer token belongs to. Tokens are compared
// by hash, so the lookup's timing says nothing about a token.
func (h *Hub) auth(r *http.Request) (string, bool) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		return "", false
	}
	name, ok := h.tokens[sha256.Sum256([]byte(token))]
	return name, ok
}

func (h *Hub) serveLink(w http.ResponseWriter, r *http.Request, name string) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(readLimit)
	ctx := r.Context()

	hctx, cancel := context.WithTimeout(ctx, helloWithin)
	f, err := link.Read(hctx, conn)
	cancel()
	var hello link.Hello
	if err != nil || f.Kind != link.KindHello || f.V != link.V || json.Unmarshal(f.Body, &hello) != nil {
		_ = conn.Close(websocket.StatusPolicyViolation, "hello: protocol v"+fmt.Sprint(link.V))
		return
	}
	if hello.Inflight <= 0 {
		hello.Inflight = 64
	}
	p := &peer{name: name, conn: conn, hello: hello, since: time.Now()}
	welcome, _ := json.Marshal(link.Welcome{Name: name})
	if err := h.write(ctx, p, link.Frame{V: link.V, Kind: link.KindWelcome, Body: welcome}); err != nil {
		return
	}
	if !h.join(p) {
		_ = conn.Close(websocket.StatusGoingAway, "shutting down")
		return
	}
	defer h.leave(p)
	h.log.Info("bird linked", "name", name, "build", hello.Build)

	ctx, stop := context.WithCancel(ctx)
	defer stop()
	go h.heartbeat(ctx, p, stop)
	for {
		f, err := link.Read(ctx, conn)
		if err != nil {
			return
		}
		switch f.Kind {
		case link.KindCall:
			h.route(ctx, name, f, func(r link.Frame) { _ = h.write(context.Background(), p, r) })
		case link.KindReply:
			h.complete(p, f)
		case link.KindEvent:
			h.fanout(p, f)
		case link.KindGoaway:
			p.draining.Store(true)
			h.log.Info("bird draining", "name", name)
		}
	}
}

// join registers p, replacing (and closing) an older link of the same bot.
func (h *Hub) join(p *peer) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false
	}
	if old := h.peers[p.name]; old != nil {
		_ = old.conn.Close(websocket.StatusPolicyViolation, "replaced by a newer link")
	}
	h.peers[p.name] = p
	return true
}

// leave forgets p: calls waiting on it fail offline, its own calls are
// dropped, since there is no one left to tell.
func (h *Hub) leave(p *peer) {
	h.mu.Lock()
	if h.peers[p.name] == p {
		delete(h.peers, p.name)
	}
	var fail []*pend
	for k, pd := range h.pending {
		switch {
		case pd.callee == p:
			fail = append(fail, pd)
			fallthrough
		case k.caller == p.name && pd.deliver != nil:
			pd.timer.Stop()
			pd.callee.inflight.Add(-1)
			delete(h.pending, k)
		}
	}
	h.mu.Unlock()
	for _, pd := range fail {
		pd.fail(link.Err(link.Offline, "callee left"))
	}
	h.log.Info("bird unlinked", "name", p.name)
}

// heartbeat measures the round trip and closes a link that stops answering.
func (h *Hub) heartbeat(ctx context.Context, p *peer, stop func()) {
	t := time.NewTicker(pingEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		pctx, cancel := context.WithTimeout(ctx, pingEvery)
		start := time.Now()
		err := p.conn.Ping(pctx)
		cancel()
		if err != nil {
			stop()
			return
		}
		p.rtt.Store(int64(time.Since(start)))
	}
}

// route sends caller's call f on to its callee, or answers it at once with
// why not. deliver takes the reply, whichever way it comes.
func (h *Hub) route(ctx context.Context, caller string, f link.Frame, deliver func(link.Frame)) {
	reply := func(err *link.Error, body json.RawMessage) {
		deliver(link.Frame{V: link.V, ID: f.ID, Kind: link.KindReply, From: Self, To: caller, Err: err, Body: body, Trace: f.Trace})
	}
	to, verb, ok := link.Target(f.Method)
	if !ok || f.ID == "" {
		reply(link.Err(link.Invalid, "a call needs an id and a <bot>.<verb> method"), nil)
		return
	}
	now := time.Now()
	deadline := time.UnixMilli(f.Deadline)
	switch {
	case f.Deadline == 0:
		deadline = now.Add(link.DefaultDeadline)
	case deadline.After(now.Add(maxDeadline)):
		deadline = now.Add(maxDeadline)
	case !deadline.After(now):
		return // already late: nobody is waiting for the answer
	}
	if to == Self {
		body, err := h.builtin(verb)
		reply(err, body)
		return
	}

	h.mu.Lock()
	callee := h.peers[to]
	var refuse *link.Error
	switch {
	case callee == nil:
		refuse = link.Err(link.Offline)
	case callee.draining.Load():
		refuse = link.Err(link.Busy, "draining")
	case !slices.Contains(callee.hello.Serves, verb):
		refuse = link.Err(link.Invalid, "no such method")
	case int(callee.inflight.Load()) >= callee.hello.Inflight:
		refuse = link.Err(link.Busy)
	case h.pending[key{caller, f.ID}] != nil:
		refuse = link.Err(link.Invalid, "id already in flight")
	}
	if refuse != nil {
		h.mu.Unlock()
		reply(refuse, nil)
		return
	}
	k := key{caller, f.ID}
	callee.inflight.Add(1)
	pd := &pend{callee: callee, deliver: deliver, fail: func(e *link.Error) { reply(e, nil) }}
	pd.timer = time.AfterFunc(time.Until(deadline), func() {
		if h.take(k, nil) != nil {
			reply(link.Err(link.Timeout), nil)
		}
	})
	h.pending[k] = pd
	h.mu.Unlock()

	f.V, f.From, f.To, f.Deadline = link.V, caller, "", deadline.UnixMilli()
	// ponytail: written from the caller's read loop, so a slow callee stalls
	// that caller for up to writeWithin; a per-peer send queue if it ever shows.
	wctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	if err := h.write(wctx, callee, f); err != nil && h.take(k, nil) != nil {
		reply(link.Err(link.Offline), nil)
	}
}

// take removes the call k if it is in flight (and, when callee is set,
// was sent to callee), returning it.
func (h *Hub) take(k key, callee *peer) *pend {
	h.mu.Lock()
	defer h.mu.Unlock()
	pd := h.pending[k]
	if pd == nil || (callee != nil && pd.callee != callee) {
		return nil
	}
	pd.timer.Stop()
	pd.callee.inflight.Add(-1)
	delete(h.pending, k)
	return pd
}

// complete hands callee's reply back to whoever made the call. A reply to
// a call that is not in flight, late or forged, goes nowhere.
func (h *Hub) complete(callee *peer, f link.Frame) {
	pd := h.take(key{f.To, f.ID}, callee)
	if pd == nil {
		return
	}
	f.V, f.From = link.V, callee.name
	pd.deliver(f)
}

// fanout stamps the event as from's and sends it to every other bot that
// subscribed to it. Best effort, like the event itself.
func (h *Hub) fanout(from *peer, f link.Frame) {
	f.V, f.From, f.Method, f.ID, f.To = link.V, from.name, from.name+"."+f.Method, "", ""
	h.mu.Lock()
	var to []*peer
	for _, p := range h.peers {
		if p != from && slices.Contains(p.hello.Subscribes, f.Method) {
			to = append(to, p)
		}
	}
	h.mu.Unlock()
	for _, p := range to {
		go func() { _ = h.write(context.Background(), p, f) }()
	}
}

// Call is cygnet calling a bird herself, as /provision does for a bird's
// manifest: routed like any bird's call, from Self, with ctx's deadline.
func (h *Hub) Call(ctx context.Context, method string, in, out any) error {
	f := link.Frame{V: link.V, ID: link.NewID(), Kind: link.KindCall, Method: method}
	if in != nil {
		body, err := json.Marshal(in)
		if err != nil {
			return err
		}
		f.Body = body
	}
	if d, ok := ctx.Deadline(); ok {
		f.Deadline = d.UnixMilli()
	}
	got := make(chan link.Frame, 1)
	h.route(ctx, Self, f, func(r link.Frame) { got <- r })
	select {
	case r := <-got:
		if r.Err != nil {
			return r.Err
		}
		if out != nil && len(r.Body) > 0 {
			return json.Unmarshal(r.Body, out)
		}
		return nil
	case <-ctx.Done():
		return link.Err(link.Timeout)
	}
}

// builtin answers calls to cygnet itself.
func (h *Hub) builtin(verb string) (json.RawMessage, *link.Error) {
	switch verb {
	case "ping":
		return json.RawMessage(`{}`), nil
	case "fleet":
		var names []string
		for _, b := range h.Fleet() {
			names = append(names, b.Name)
		}
		body, _ := json.Marshal(map[string][]string{"linked": names})
		return body, nil
	}
	return nil, link.Err(link.Invalid, "no such method")
}

func (h *Hub) write(ctx context.Context, p *peer, f link.Frame) error {
	ctx, cancel := context.WithTimeout(ctx, writeWithin)
	defer cancel()
	return link.Write(ctx, p.conn, f)
}

// serveCall is a call over plain HTTP, for a script or a bot without a link:
// the body is a call frame, the answer its reply frame.
func (h *Hub) serveCall(w http.ResponseWriter, r *http.Request, caller string) {
	var f link.Frame
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, readLimit)).Decode(&f); err != nil {
		http.Error(w, "bad frame", http.StatusBadRequest)
		return
	}
	if f.ID == "" {
		f.ID = link.NewID()
	}
	f.Kind = link.KindCall
	got := make(chan link.Frame, 1)
	h.route(r.Context(), caller, f, func(r link.Frame) { got <- r })
	var reply link.Frame
	select {
	case reply = <-got:
	case <-r.Context().Done():
		return
	case <-time.After(maxDeadline + time.Second):
		reply = link.Frame{Err: link.Err(link.Timeout)}
	}
	reply.V, reply.ID, reply.Kind, reply.To = link.V, f.ID, link.KindReply, caller
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(reply)
}

// Fleet is every linked bot, by name.
func (h *Hub) Fleet() []Bird {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Bird, 0, len(h.peers))
	for _, p := range h.peers {
		out = append(out, Bird{
			Name: p.name, Build: p.hello.Build, Since: p.since, RTT: time.Duration(p.rtt.Load()),
			Inflight: int(p.inflight.Load()), Draining: p.draining.Load(),
		})
	}
	slices.SortFunc(out, func(a, b Bird) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// Close says goaway to every bird and closes their links; they come back
// to the next cygnet on their own.
func (h *Hub) Close() {
	h.mu.Lock()
	h.closed = true
	peers := make([]*peer, 0, len(h.peers))
	for _, p := range h.peers {
		peers = append(peers, p)
	}
	h.mu.Unlock()
	for _, p := range peers {
		_ = h.write(context.Background(), p, link.Frame{V: link.V, Kind: link.KindGoaway})
		_ = p.conn.Close(websocket.StatusGoingAway, "goaway")
	}
}

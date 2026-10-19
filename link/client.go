package link

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	mrand "math/rand/v2"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// DefaultDeadline is how long a call waits when its context has no deadline.
const DefaultDeadline = 10 * time.Second

// readLimit is the largest frame either side accepts.
const readLimit = 1 << 20

// Config is how a bot reaches cygnet.
type Config struct {
	// URL is the hub's link endpoint, e.g. ws://cygnet:7700/v1/link.
	URL string
	// Token is this bot's token; the hub knows the bot by it.
	Token string
	// Build is shown on cygnet's /status, e.g. the commit.
	Build string
	// Inflight caps the calls this bot takes at once. 0 means 64.
	Inflight int
	// Attest is this build's attestation, from its CI (DecodeAttestation),
	// or nil for a build that has none.
	Attest *Attestation
	Log    *slog.Logger
}

// Call is an incoming call as a handler sees it. From and Trust are
// stamped by cygnet: From from the caller's token, Trust from how far she
// checked its build.
type Call struct {
	ID, From, Method string
	Trust            Trust
	Body             json.RawMessage
}

// Bind decodes the call's body into v.
func (c Call) Bind(v any) error {
	if len(c.Body) == 0 {
		return nil
	}
	if err := json.Unmarshal(c.Body, v); err != nil {
		return Err(Invalid, "body")
	}
	return nil
}

// Event is an event as a subscriber sees it. Method is "<bot>.<event>" and
// From is the bot that emitted it, both stamped by cygnet, as is Trust.
type Event struct {
	From, Method string
	Trust        Trust
	Body         json.RawMessage
}

// Handler answers a call. Its context ends at the call's deadline. Return a
// *Error to tell the caller why; any other error reaches it as Internal with
// no reason, so nothing about this bot's insides leaves it.
type Handler func(ctx context.Context, c Call) (any, error)

// Client is one bot's link to cygnet. Register handlers and subscriptions,
// then Run it; Call and Emit are safe from any goroutine once it runs.
type Client struct {
	cfg      Config
	handlers map[string]Handler
	on       map[string]func(Event)

	mu      sync.Mutex
	conn    *websocket.Conn
	name    string
	pending map[string]chan Frame
}

// New makes a client. Nothing connects until Run.
func New(cfg Config) *Client {
	if cfg.Inflight <= 0 {
		cfg.Inflight = 64
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	return &Client{cfg: cfg, handlers: map[string]Handler{}, on: map[string]func(Event){}, pending: map[string]chan Frame{}}
}

// Handle answers calls to "<this bot>.<verb>". Register before Run.
func (c *Client) Handle(verb string, h Handler) { c.handlers[verb] = h }

// On receives the event "<bot>.<event>". Register before Run.
func (c *Client) On(event string, fn func(Event)) { c.on[event] = fn }

// Name is the name cygnet knows this bot by while linked, and empty while not.
func (c *Client) Name() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.name
}

// Linked is whether the link is up right now.
func (c *Client) Linked() bool { return c.Name() != "" }

// Run keeps the link up until ctx ends, reconnecting with backoff. On the
// way out it says goaway, so cygnet stops sending calls before it closes.
func (c *Client) Run(ctx context.Context) error {
	wait := time.Second
	for {
		start := time.Now()
		err := c.session(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if time.Since(start) > 30*time.Second {
			wait = time.Second
		}
		c.cfg.Log.Warn("link down", "err", err, "retry", wait)
		// Jitter, so a restarted hub isn't met by every bird at once.
		t := time.NewTimer(wait/2 + mrand.N(wait/2+1))
		select {
		case <-ctx.Done():
			t.Stop()
			return nil
		case <-t.C:
		}
		wait = min(2*wait, 30*time.Second)
	}
}

func (c *Client) session(ctx context.Context) error {
	conn, _, err := websocket.Dial(ctx, c.cfg.URL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + c.cfg.Token}},
	})
	if err != nil {
		return err
	}
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(readLimit)

	serves := make([]string, 0, len(c.handlers))
	for v := range c.handlers {
		serves = append(serves, v)
	}
	subs := make([]string, 0, len(c.on))
	for e := range c.on {
		subs = append(subs, e)
	}
	slices.Sort(serves)
	slices.Sort(subs)
	hello, _ := json.Marshal(Hello{Build: c.cfg.Build, Serves: serves, Subscribes: subs, Inflight: c.cfg.Inflight, Attest: c.cfg.Attest})
	if err := write(ctx, conn, Frame{V: V, Kind: KindHello, Body: hello}); err != nil {
		return err
	}
	f, err := read(ctx, conn)
	if err != nil {
		return err
	}
	var w Welcome
	if f.Kind != KindWelcome || json.Unmarshal(f.Body, &w) != nil {
		return fmt.Errorf("link: expected welcome, got %q", f.Kind)
	}
	c.mu.Lock()
	c.conn, c.name = conn, w.Name
	c.mu.Unlock()
	c.cfg.Log.Info("link up", "as", w.Name, "trust", string(w.Trust))
	defer c.drop(conn)

	// Leaving: goaway first, then close once cygnet has seen it.
	stop := context.AfterFunc(ctx, func() {
		wctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = write(wctx, conn, Frame{V: V, Kind: KindGoaway})
		_ = conn.Close(websocket.StatusNormalClosure, "goaway")
	})
	defer stop()

	for {
		f, err := read(ctx, conn)
		if err != nil {
			return err
		}
		switch f.Kind {
		case KindReply:
			c.mu.Lock()
			ch := c.pending[f.ID]
			delete(c.pending, f.ID)
			c.mu.Unlock()
			if ch != nil {
				ch <- f
			}
		case KindCall:
			go c.answer(ctx, conn, f)
		case KindEvent:
			if fn := c.on[f.Method]; fn != nil {
				go fn(Event{From: f.From, Trust: f.Trust, Method: f.Method, Body: f.Body})
			}
		case KindGoaway:
			return errors.New("link: cygnet is going away")
		}
	}
}

// drop forgets conn and fails every call still waiting on it.
func (c *Client) drop(conn *websocket.Conn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != conn {
		return
	}
	c.conn = nil
	c.name = ""
	for id, ch := range c.pending {
		ch <- Frame{ID: id, Kind: KindReply, Err: Err(Offline, "link lost")}
		delete(c.pending, id)
	}
}

func (c *Client) answer(ctx context.Context, conn *websocket.Conn, f Frame) {
	reply := Frame{V: V, ID: f.ID, Kind: KindReply, To: f.From, Trace: f.Trace}
	_, verb, _ := Target(f.Method)
	h := c.handlers[verb]
	if h == nil {
		reply.Err = Err(Invalid, "no such method")
	} else {
		hctx, cancel := context.WithDeadline(ctx, time.UnixMilli(f.Deadline))
		out, err := h(hctx, Call{ID: f.ID, From: f.From, Trust: f.Trust, Method: f.Method, Body: f.Body})
		cancel()
		var le *Error
		switch {
		case errors.As(err, &le):
			reply.Err = le
		case err != nil:
			c.cfg.Log.Warn("handler failed", "method", f.Method, "from", f.From, "err", err)
			reply.Err = Err(Internal)
		case out != nil:
			if reply.Body, err = json.Marshal(out); err != nil {
				reply.Err = Err(Internal)
			}
		}
	}
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_ = write(wctx, conn, reply)
}

// NewID is a fresh call ID. Keep it to retry the same call with CallID.
func NewID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Call calls method ("<bot>.<verb>") with in as the body and decodes the
// reply into out (either may be nil). It fails with a *Error.
func (c *Client) Call(ctx context.Context, method string, in, out any) error {
	return c.CallID(ctx, NewID(), method, in, out)
}

// CallID is Call with a chosen ID. Retrying with the same ID lets a callee
// that keeps recent IDs treat the retry as the call it already did.
func (c *Client) CallID(ctx context.Context, id, method string, in, out any) error {
	if _, _, ok := Target(method); !ok {
		return Err(Invalid, "method is <bot>.<verb>")
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(DefaultDeadline)
	}
	f := Frame{V: V, ID: id, Kind: KindCall, Method: method, Deadline: deadline.UnixMilli()}
	if in != nil {
		var err error
		if f.Body, err = json.Marshal(in); err != nil {
			return Err(Invalid, "body")
		}
	}
	ch := make(chan Frame, 1)
	c.mu.Lock()
	conn := c.conn
	if conn != nil {
		c.pending[id] = ch
	}
	c.mu.Unlock()
	if conn == nil {
		return Err(Offline, "not linked")
	}
	forget := func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	if err := write(ctx, conn, f); err != nil {
		forget()
		return Err(Offline, "link lost")
	}
	select {
	case r := <-ch:
		if r.Err != nil {
			return r.Err
		}
		if out != nil && len(r.Body) > 0 {
			return json.Unmarshal(r.Body, out)
		}
		return nil
	case <-ctx.Done():
		forget()
		return Err(Timeout)
	}
}

// Emit announces event to every bot subscribed to "<this bot>.<event>".
// Best effort: nobody acknowledges an event.
func (c *Client) Emit(ctx context.Context, event string, body any) error {
	f := Frame{V: V, Kind: KindEvent, Method: event}
	if body != nil {
		var err error
		if f.Body, err = json.Marshal(body); err != nil {
			return Err(Invalid, "body")
		}
	}
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return Err(Offline, "not linked")
	}
	return write(ctx, conn, f)
}

func write(ctx context.Context, conn *websocket.Conn, f Frame) error {
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, b)
}

func read(ctx context.Context, conn *websocket.Conn) (Frame, error) {
	_, b, err := conn.Read(ctx)
	if err != nil {
		return Frame{}, err
	}
	var f Frame
	if err := json.Unmarshal(b, &f); err != nil {
		return Frame{}, fmt.Errorf("link: bad frame: %w", err)
	}
	return f, nil
}

// Write and Read are the frame codec, for the hub's side of the socket.
var (
	Write = write
	Read  = read
)

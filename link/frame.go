// Package link is how a bird talks to the rest of the fleet through cygnet:
// the protocol's frames and a client that keeps one connection to the hub,
// answers calls, makes them and emits and receives events.
//
// It depends on the standard library and coder/websocket only, so a bot on
// any Discord library can import it without taking on cygnet's.
package link

import (
	"encoding/json"
	"errors"
	"strings"
)

// V is the protocol version this package speaks. The hub refuses a hello
// from any other.
const V = 1

// Kind is what a frame is for.
type Kind string

const (
	// KindHello is the first frame a bot sends; its body is a Hello.
	KindHello Kind = "hello"
	// KindWelcome is the hub's answer to a hello; its body is a Welcome.
	KindWelcome Kind = "welcome"
	// KindCall asks the bot named by Method's first segment to do something.
	KindCall Kind = "call"
	// KindReply answers a call, by the call's ID.
	KindReply Kind = "reply"
	// KindEvent is a fact a bot announces to whoever subscribed to it.
	KindEvent Kind = "event"
	// KindGoaway says the sender is draining: it takes no new calls.
	KindGoaway Kind = "goaway"
)

// Frame is the one message shape on the wire, as JSON.
type Frame struct {
	V    int    `json:"v"`
	ID   string `json:"id,omitempty"`
	Kind Kind   `json:"kind"`
	// From is stamped by the hub from the connection's token; whatever a bot
	// puts here is overwritten.
	From string `json:"from,omitempty"`
	// Trust is stamped by the hub with how far it checked From's build
	// (attest.go); whatever a bot puts here is overwritten too.
	Trust Trust `json:"trust,omitempty"`
	// To is set on a reply only: the caller it goes back to.
	To string `json:"to,omitempty"`
	// Method is "<bot>.<verb>" on a call and "<bot>.<event>" on an event.
	Method string          `json:"method,omitempty"`
	Body   json.RawMessage `json:"body,omitempty"`
	Err    *Error          `json:"err,omitempty"`
	// Deadline is when a call stops mattering, in Unix milliseconds.
	Deadline int64  `json:"deadline,omitempty"`
	Trace    string `json:"trace,omitempty"`
}

// Hello is what a bot can do, sent once when it connects.
type Hello struct {
	Build string `json:"build,omitempty"`
	// Serves is the verbs it answers: "ping" means calls to "<bot>.ping".
	Serves []string `json:"serves,omitempty"`
	// Subscribes is the full event names it wants: "skua.purge.done".
	Subscribes []string `json:"subscribes,omitempty"`
	// Inflight is how many calls it takes at once; more are refused busy.
	Inflight int `json:"inflight,omitempty"`
	// Attest is the bird's signed build. A bird whose release key cygnet
	// pins can't link without a good one.
	Attest *Attestation `json:"attest,omitempty"`
}

// Welcome is the hub's answer: the name the token belongs to, and how far
// cygnet trusts this link.
type Welcome struct {
	Name  string `json:"name"`
	Trust Trust  `json:"trust,omitempty"`
}

// Code says why a call failed.
type Code string

const (
	// Offline: the callee is not connected.
	Offline Code = "offline"
	// Timeout: the deadline passed before a reply.
	Timeout Code = "timeout"
	// Denied: the callee would not do it for this caller.
	Denied Code = "denied"
	// Busy: the callee is at its in-flight cap or draining.
	Busy Code = "busy"
	// Invalid: the call itself is wrong (no such method, bad body).
	Invalid Code = "invalid"
	// Internal: the callee failed. The reason stays on its side.
	Internal Code = "internal"
)

// Error is a call's failure as the caller sees it.
type Error struct {
	Code   Code   `json:"code"`
	Reason string `json:"reason,omitempty"`
}

func (e *Error) Error() string {
	if e.Reason == "" {
		return string(e.Code)
	}
	return string(e.Code) + ": " + e.Reason
}

// Is lets errors.Is match on the code alone: errors.Is(err, link.Err(link.Busy)).
func (e *Error) Is(target error) bool {
	var t *Error
	return errors.As(target, &t) && t.Code == e.Code
}

// Err is an Error with code c and reason why, for a handler to return.
func Err(c Code, why ...string) *Error {
	return &Error{Code: c, Reason: strings.Join(why, " ")}
}

// Target is the bot a method belongs to: "skua" for "skua.ping".
func Target(method string) (bot, rest string, ok bool) {
	bot, rest, ok = strings.Cut(method, ".")
	return bot, rest, ok && bot != "" && rest != ""
}

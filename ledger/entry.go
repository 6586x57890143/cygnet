// Package ledger is the log every bird keeps: what it did in a server, to
// whom, why and on whose word, as typed entries rather than strings to parse
// back, rendered as one card shape for every bird and chained by sha256 so a
// range can be checked rather than trusted.
//
// It is shared code, not a shared service. Each bird stores its own entries
// (Store), posts its own cards to the server's log channel and answers
// questions about them itself; nothing here talks to cygnet or to another
// bird, so a bird logs exactly the same with the hub down. It depends on
// disgo and the standard library only.
package ledger

import (
	"strings"
	"time"
)

// Mood is how an entry reads at a glance. A Brand turns it into a colour.
type Mood string

const (
	// MoodOK is something done as asked.
	MoodOK Mood = "ok"
	// MoodInfo is a readout or a routine automatic action.
	MoodInfo Mood = "info"
	// MoodWarn is done, but somebody should look.
	MoodWarn Mood = "warn"
	// MoodError is something that failed.
	MoodError Mood = "error"
	// MoodIdle is a bird doing what it was told to do nothing about: a
	// pause, a dry run. Not a failure.
	MoodIdle Mood = "idle"
	// MoodDanger is irreversible: a channel deleted, a member banned. The
	// entries somebody goes looking for after the thing is gone.
	MoodDanger Mood = "danger"
)

// rank orders moods by how loud they are, for a batch's colour.
var rank = map[Mood]int{MoodIdle: 0, MoodInfo: 1, MoodOK: 2, MoodWarn: 3, MoodError: 4, MoodDanger: 5}

// louder is whichever of a and b a reader most needs to see.
func louder(a, b Mood) Mood {
	if rank[b] > rank[a] {
		return b
	}
	return a
}

// ActorKind is who an entry acted for.
type ActorKind string

const (
	// ActorMember is a person: a command, a button, a form.
	ActorMember ActorKind = "member"
	// ActorAuto is the bird on its own: a timer, a sweep, a rule.
	ActorAuto ActorKind = "auto"
)

// Actor is who an entry acted for. ID is a user ID for a member and empty
// for the bird itself.
type Actor struct {
	Kind ActorKind `json:"kind"`
	ID   string    `json:"id,omitempty"`
}

// Member is the actor for a person.
func Member(id string) Actor { return Actor{Kind: ActorMember, ID: id} }

// Auto is the actor for the bird acting on its own.
func Auto() Actor { return Actor{Kind: ActorAuto} }

// RefKind is what a Ref points at.
type RefKind string

const (
	RefUser    RefKind = "user"
	RefChannel RefKind = "channel"
	RefRole    RefKind = "role"
	// RefMessage is a message, its ID written "<channel>/<message>" since a
	// link to one needs both.
	RefMessage RefKind = "message"
)

// Ref is something an entry is about, kept as an ID and rendered as a
// mention, so a search by ID matches the row exactly and the card reads as
// names.
type Ref struct {
	Kind RefKind `json:"kind"`
	ID   string  `json:"id"`
	// Label is what this ref is to the entry ("kept", "restored"), in the
	// shared voice. Refs with no label are the entry's who and where, on one
	// line; refs sharing a label are a line of their own, "kept: @a, @b".
	Label string `json:"label,omitempty"`
}

// As is r labelled.
func (r Ref) As(label string) Ref {
	r.Label = label
	return r
}

func User(id string) Ref    { return Ref{Kind: RefUser, ID: id} }
func Channel(id string) Ref { return Ref{Kind: RefChannel, ID: id} }
func Role(id string) Ref    { return Ref{Kind: RefRole, ID: id} }

// Message is a message in channel.
func Message(channel, message string) Ref { return Ref{Kind: RefMessage, ID: channel + "/" + message} }

// Fact is one line of the card's grid: a short label and a plain value.
// Values sit in a code block, where a mention shows as its raw ID, so who
// and where belong in Subjects, never here.
type Fact struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Change is one setting moved, shown in the grid as from and to.
type Change struct {
	Field string `json:"field"`
	From  string `json:"from"`
	To    string `json:"to"`
}

// Button is something a reader can do about an entry: undo it, open a
// member's sheet. CustomID is the bird's own to route; URL makes a link
// button instead.
type Button struct {
	Label    string `json:"label"`
	CustomID string `json:"custom_id,omitempty"`
	URL      string `json:"url,omitempty"`
	// Danger draws it red: an undo that cannot itself be undone.
	Danger bool `json:"danger,omitempty"`
}

// Entry is one thing a bird did.
type Entry struct {
	// ID is the bird's own for the row; Cause on another entry names it.
	ID    string    `json:"id,omitempty"`
	Bird  string    `json:"bird"`
	Guild string    `json:"guild"`
	At    time.Time `json:"at"`
	Actor Actor     `json:"actor"`
	// Action is "<module>.<verb>": roles.jail, provision.drift.
	Action   string `json:"action"`
	Subjects []Ref  `json:"subjects,omitempty"`
	Mood     Mood   `json:"mood"`
	// Summary is the one line a reader takes away, in the shared voice:
	// lowercase, no closing full stop.
	Summary string   `json:"summary"`
	Facts   []Fact   `json:"facts,omitempty"`
	Changes []Change `json:"changes,omitempty"`
	// Reason is why, as the actor gave it.
	Reason string `json:"reason,omitempty"`
	// Cause names the entry this one follows from, as "<bird>:<id>", which
	// may be another bird's: a filter hit, then the jail it led to.
	Cause   string   `json:"cause,omitempty"`
	Buttons []Button `json:"buttons,omitempty"`
}

// Module is the part of Action before the dot.
func (e Entry) Module() string {
	m, _, _ := strings.Cut(e.Action, ".")
	return m
}

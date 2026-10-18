package ledger

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// The limits a card is built to. A label leads its line in bold, and a long
// one pushes every value it heads off a phone's first line.
const (
	maxLabel   = 10
	maxSummary = 200
	maxReason  = 500
	maxFacts   = 12
	maxButtons = 5
)

// generated is em and en dash, ellipsis and curly quotes, spelled as code
// points so this file passes the prose check itself.
var generated = string([]rune{0x2014, 0x2013, 0x2026, 0x2018, 0x2019, 0x201c, 0x201d})

// Validate holds an entry to the voice every bird shares and to what a card
// can carry. A bird runs it in its tests over every entry it can write, the
// way a command with no tier fails boot: a bad entry is found in CI, never
// as a broken card in somebody's log channel. Reason is not held to the
// voice, since it is the actor's own words.
func Validate(e Entry) error {
	var errs []error
	bad := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	if e.Bird == "" || e.Guild == "" {
		bad("bird and guild are required")
	}
	if mod, verb, ok := strings.Cut(e.Action, "."); !ok || mod == "" || verb == "" || e.Action != strings.ToLower(e.Action) {
		bad("action %q: want lowercase <module>.<verb>", e.Action)
	}
	if _, ok := rank[e.Mood]; !ok {
		bad("mood %q: unknown", e.Mood)
	}
	switch {
	case e.Actor.Kind == ActorMember && e.Actor.ID == "":
		bad("a member actor needs an ID")
	case e.Actor.Kind != ActorMember && e.Actor.Kind != ActorAuto:
		bad("actor kind %q: unknown", e.Actor.Kind)
	}
	if e.Summary == "" || utf8.RuneCountInString(e.Summary) > maxSummary {
		bad("summary: empty or over %d", maxSummary)
	}
	if err := voice(e.Summary); err != nil {
		bad("summary %q: %v", e.Summary, err)
	}
	if utf8.RuneCountInString(e.Reason) > maxReason {
		bad("reason over %d", maxReason)
	}
	if len(e.Facts)+len(e.Changes) > maxFacts {
		bad("%d fact lines, over %d", len(e.Facts)+len(e.Changes), maxFacts)
	}
	for _, f := range e.Facts {
		if err := factLine(f.Label, f.Value); err != nil {
			bad("fact %q: %v", f.Label, err)
		}
	}
	for _, c := range e.Changes {
		if err := factLine(c.Field, c.From+c.To); err != nil {
			bad("change %q: %v", c.Field, err)
		}
	}
	for _, r := range e.Subjects {
		if r.ID == "" || mention(e.Guild, r, "") == "" {
			bad("subject %+v: unknown kind or no ID", r)
		}
		if r.Label != "" && (utf8.RuneCountInString(r.Label) > maxLabel || voice(r.Label) != nil) {
			bad("subject label %q: over %d or not in the voice", r.Label, maxLabel)
		}
	}
	if len(e.Buttons) > maxButtons {
		bad("%d buttons, over %d", len(e.Buttons), maxButtons)
	}
	for _, b := range e.Buttons {
		if b.Label == "" || (b.CustomID == "") == (b.URL == "") {
			bad("button %q: needs a label and exactly one of custom ID and URL", b.Label)
		}
		if err := voice(b.Label); err != nil {
			bad("button %q: %v", b.Label, err)
		}
	}
	return errors.Join(errs...)
}

// voice is the shared rule for words a bird writes: no closing full stop,
// no exclamation marks outside backticks, nothing that reads
// as generated.
func voice(s string) error {
	plain := stripCode(s)
	switch {
	case strings.HasSuffix(s, "."):
		return errors.New("ends on a full stop")
	case strings.Contains(plain, "!"):
		return errors.New("exclamation mark")
	case strings.ContainsAny(s, generated):
		return errors.New("em dash, ellipsis or curly quote")
	}
	return nil
}

// stripCode drops `code` spans, which are typed exactly and may be any case.
func stripCode(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		if r == '`' {
			in = !in
			continue
		}
		if !in {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func factLine(label, value string) error {
	switch {
	case label == "" || utf8.RuneCountInString(label) > maxLabel:
		return fmt.Errorf("label empty or over %d", maxLabel)
	case voice(label) != nil:
		return fmt.Errorf("label: %v", voice(label))
	case strings.Contains(label+value, "\n"):
		return errors.New("a newline would split the fact from its label")
	}
	return nil
}

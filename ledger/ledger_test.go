package ledger_test

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/6586x57890143/cygnet/ledger"
	"github.com/6586x57890143/cygnet/ledger/ledgertest"
)

var update = flag.Bool("update", false, "rewrite the golden cards")

var brand = ledger.Brand{
	Name: "tester",
	Icon: func(m string) string { return "<:mod_" + m + ":1>" },
	Color: func(m ledger.Mood) int {
		return map[ledger.Mood]int{ledger.MoodOK: 1, ledger.MoodWarn: 2, ledger.MoodDanger: 3}[m]
	},
	Thumbnail: func(m ledger.Mood) string { return "attachment://tester_" + string(m) + ".png" },
	Rule:      "attachment://tester_rule.png",
}

func jail(user string) ledger.Entry {
	return ledger.Entry{
		ID: "41", Bird: "tester", Guild: "100", At: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC),
		Actor: ledger.Member("175928847299117063"), Action: "roles.jail", Mood: ledger.MoodOK,
		Subjects: []ledger.Ref{ledger.User(user), ledger.Message("200", "300"), ledger.Role("501").As("kept"), ledger.Role("502").As("kept"), ledger.Role("503").As("restored")},
		Summary:  "jailed for 2h; their 4 roles are kept for release",
		Facts:    []ledger.Fact{{Label: "until", Value: "14:00 utc"}, {Label: "kept", Value: "booster, a role above merlin's own, and one more that wraps"}},
		Changes:  []ledger.Change{{Field: "marker", From: "none", To: "jailed"}},
		Reason:   "spam\nlinks",
		Cause:    "tester:40",
		Buttons:  []ledger.Button{{Label: "release", CustomID: "roles:release:1001", Danger: true}, {Label: "sheet", URL: "https://example.com"}},
	}
}

// golden compares card's JSON with testdata/name.json.
func golden(t *testing.T, name string, card ledger.Card) {
	t.Helper()
	got, err := json.MarshalIndent(card, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", name+".json")
	if *update {
		if err := os.WriteFile(path, append(got, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if strings.TrimSpace(string(want)) != string(got) {
		t.Fatalf("%s drifted from its golden card:\n%s", name, got)
	}
}

func TestRenderedCards(t *testing.T) {
	golden(t, "single", ledger.Render(brand, jail("1001")))

	auto := jail("1002")
	auto.Actor, auto.Facts, auto.Changes, auto.Buttons, auto.Reason, auto.Cause = ledger.Auto(), nil, nil, nil, "", ""
	golden(t, "automatic", ledger.Render(ledger.Brand{}, auto))

	second := jail("1003")
	second.Mood, second.Reason = ledger.MoodDanger, "other"
	golden(t, "batch", ledger.Render(brand, jail("1001"), second))
}

func TestRenderEmpty(t *testing.T) {
	if c := ledger.Render(brand); c.Embed.Title != "" {
		t.Fatalf("no entries, no card: %+v", c)
	}
}

func TestEveryExampleIsValid(t *testing.T) {
	if err := ledger.Validate(jail("1001")); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Validate(ledgertest.Entry("g", 1)); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRefuses(t *testing.T) {
	for name, edit := range map[string]func(*ledger.Entry){
		"no bird":           func(e *ledger.Entry) { e.Bird = "" },
		"action shape":      func(e *ledger.Entry) { e.Action = "jail" },
		"action case":       func(e *ledger.Entry) { e.Action = "Roles.jail" },
		"mood":              func(e *ledger.Entry) { e.Mood = "loud" },
		"member without id": func(e *ledger.Entry) { e.Actor = ledger.Actor{Kind: ledger.ActorMember} },
		"actor kind":        func(e *ledger.Entry) { e.Actor = ledger.Actor{Kind: "robot"} },
		"full stop":         func(e *ledger.Entry) { e.Summary = "jailed." },
		"exclamation":       func(e *ledger.Entry) { e.Summary = "jailed!" },
		"generated":         func(e *ledger.Entry) { e.Summary = "jailed " + string(rune(0x2014)) + " for spam" },
		"empty summary":     func(e *ledger.Entry) { e.Summary = "" },
		"long reason":       func(e *ledger.Entry) { e.Reason = strings.Repeat("x", 501) },
		"long label":        func(e *ledger.Entry) { e.Facts[0].Label = "much too long a label" },
		"split fact":        func(e *ledger.Entry) { e.Facts[0].Value = "one\ntwo" },
		"too many lines":    func(e *ledger.Entry) { e.Facts = make([]ledger.Fact, 12) },
		"bad subject":       func(e *ledger.Entry) { e.Subjects = []ledger.Ref{{Kind: "planet", ID: "1"}} },
		"message no slash":  func(e *ledger.Entry) { e.Subjects = []ledger.Ref{{Kind: ledger.RefMessage, ID: "1"}} },
		"two targets":       func(e *ledger.Entry) { e.Buttons[0].URL = "https://x" },
		"six buttons": func(e *ledger.Entry) {
			e.Buttons = append(e.Buttons, e.Buttons[0], e.Buttons[0], e.Buttons[0], e.Buttons[0])
		},
	} {
		e := jail("1001")
		edit(&e)
		if ledger.Validate(e) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	e := jail("1001")
	e.Summary = "set `MERLIN_TOKEN` again"
	if err := ledger.Validate(e); err != nil {
		t.Errorf("code spans may be any case: %v", err)
	}
}

func TestChainCatchesAnEditAndAGap(t *testing.T) {
	var rs []ledger.Record
	var last ledger.Record
	for i := range 3 {
		last = ledger.Next(last, ledgertest.Entry("g", i))
		rs = append(rs, last)
	}
	if err := ledger.Verify(ledger.Hash{}, rs); err != nil {
		t.Fatal(err)
	}
	edited := append([]ledger.Record(nil), rs...)
	edited[1].Entry.Summary = "released"
	if err := ledger.Verify(ledger.Hash{}, edited); err == nil || !strings.Contains(err.Error(), "record 2 was changed") {
		t.Fatalf("an edit: %v", err)
	}
	if err := ledger.Verify(ledger.Hash{}, []ledger.Record{rs[0], rs[2]}); err == nil || !strings.Contains(err.Error(), "record 3 does not follow") {
		t.Fatalf("a gap: %v", err)
	}
	// What Postgres hands back: the same instant in another zone, to the
	// microsecond.
	moved := rs[0]
	moved.Entry.At = moved.Entry.At.Truncate(time.Microsecond).In(time.FixedZone("x", 3600))
	if ledger.Chain(ledger.Hash{}, moved.Entry) != rs[0].Hash {
		t.Fatal("a row read back from the database must hash as written")
	}
}

func TestMemKeepsTheContract(t *testing.T) {
	ledgertest.Run(t, func(*testing.T) ledger.Store { return ledgertest.NewMem() })
}

func TestReason(t *testing.T) {
	if got := ledger.Reason(jail("1")); got != "tester · roles.jail · by 175928847299117063 · spam\nlinks" {
		t.Fatalf("got %q", got)
	}
	e := jail("1")
	e.Actor, e.Reason = ledger.Auto(), strings.Repeat("y", 600)
	got := ledger.Reason(e)
	if !strings.HasPrefix(got, "tester · roles.jail · automatic · yyy") || len([]rune(got)) != 512 || !strings.HasSuffix(got, "...") {
		t.Fatalf("got %d runes: %q", len([]rune(got)), got[:40])
	}
}

// fakeChannel records posts and edits.
type fakeChannel struct {
	posts   int
	edits   map[string]int
	cards   map[string]ledger.Card
	editErr error
}

func (f *fakeChannel) batcher(now *time.Time) *ledger.Batcher {
	f.edits, f.cards = map[string]int{}, map[string]ledger.Card{}
	return &ledger.Batcher{
		Brand: brand,
		Post: func(_ context.Context, _ string, c ledger.Card) (string, error) {
			f.posts++
			id := string(rune('a' + f.posts - 1))
			f.cards[id] = c
			return id, nil
		},
		Edit: func(_ context.Context, _, msg string, c ledger.Card) error {
			if f.editErr != nil {
				return f.editErr
			}
			f.edits[msg]++
			f.cards[msg] = c
			return nil
		},
		Now: func() time.Time { return *now },
	}
}

// text is every word on the card's embed as one string.
func text(c ledger.Card) string {
	out := []string{c.Embed.Title, c.Embed.Description}
	for _, f := range c.Embed.Fields {
		out = append(out, f.Name, f.Value)
	}
	return strings.Join(out, "\n")
}

func TestBatcherFoldsARunIntoOneCard(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	var ch fakeChannel
	b := ch.batcher(&now)
	ctx := context.Background()

	for _, u := range []string{"1", "2", "3"} {
		if err := b.Add(ctx, jail(u)); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Second)
	}
	if ch.posts != 1 || ch.edits["a"] != 2 {
		t.Fatalf("three jails in 3s: %d posts, %d edits; want one card edited twice", ch.posts, ch.edits["a"])
	}
	if got := text(ch.cards["a"]); !strings.Contains(got, "release · 3") || !strings.Contains(got, "<@3>") {
		t.Fatalf("the card did not grow: %s", got)
	}

	// Another actor, another action and another guild are their own cards.
	other := jail("4")
	other.Actor = ledger.Auto()
	_ = b.Add(ctx, other)
	rel := jail("5")
	rel.Action = "roles.release"
	_ = b.Add(ctx, rel)
	g := jail("6")
	g.Guild = "101"
	_ = b.Add(ctx, g)
	if ch.posts != 4 {
		t.Fatalf("%d posts, want 4", ch.posts)
	}

	// Past the window the next jail starts a new card.
	now = now.Add(ledger.DefaultWindow)
	_ = b.Add(ctx, jail("7"))
	if ch.posts != 5 {
		t.Fatalf("after the window: %d posts, want 5", ch.posts)
	}
}

func TestBatcherCapsACard(t *testing.T) {
	now := time.Now()
	var ch fakeChannel
	b := ch.batcher(&now)
	b.Max = 2
	for _, u := range []string{"1", "2", "3"} {
		_ = b.Add(context.Background(), jail(u))
	}
	if ch.posts != 2 {
		t.Fatalf("max 2: %d posts, want 2", ch.posts)
	}
}

func TestBatcherPostsAfreshWhenTheEditFails(t *testing.T) {
	now := time.Now()
	var ch fakeChannel
	b := ch.batcher(&now)
	_ = b.Add(context.Background(), jail("1"))
	ch.editErr = errors.New("unknown message")
	if err := b.Add(context.Background(), jail("2")); err != nil {
		t.Fatal(err)
	}
	if ch.posts != 2 {
		t.Fatalf("a card that won't edit must not lose the entry: %d posts", ch.posts)
	}
}

func TestBatcherReportsAFailedPost(t *testing.T) {
	b := ledger.Batcher{Post: func(context.Context, string, ledger.Card) (string, error) {
		return "", errors.New("missing access")
	}}
	if err := b.Add(context.Background(), jail("1")); err == nil {
		t.Fatal("a post that failed must say so")
	}
}

func TestLongBatchStaysUnderDiscordsLimit(t *testing.T) {
	es := make([]ledger.Entry, 25)
	for i := range es {
		es[i] = jail("1")
		es[i].Summary = strings.Repeat("long words ", 18)
	}
	if n := len([]rune(text(ledger.Render(brand, es...)))); n > 4000 {
		t.Fatalf("%d runes", n)
	}
}

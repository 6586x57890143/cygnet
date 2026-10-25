package perf

import (
	"log/slog"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/core"
	"github.com/6586x57890143/cygnet/internal/core/coretest"
	"github.com/6586x57890143/cygnet/internal/obs"
)

func module(rec *obs.Recorder) *Module {
	m := New(rec)
	m.runtime = func() (time.Duration, time.Duration) { return 40 * time.Microsecond, 300 * time.Microsecond }
	return m
}

// The readout lists the slowest stage first, keeps every key on one line in
// a column, and scales each bar to the slowest.
func TestReadout(t *testing.T) {
	rec := obs.New()
	rec.Add("whisper", obs.Run, 3*time.Millisecond)
	rec.Add("put reactions/@me", obs.Wait, 250*time.Millisecond)
	rec.Add("whisper", obs.HTTP, 80*time.Millisecond)
	for range 1500 {
		rec.Add("bird", obs.In, 40*time.Millisecond)
	}
	rows, runtime := module(rec).readout()
	var got strings.Builder
	for _, r := range append(rows, runtime...) {
		if n := utf8.RuneCountInString(r[0]); n > labelW {
			t.Errorf("key %q is %d long", r[0], n)
		}
		if strings.Contains(r[1], "\n") {
			t.Errorf("%q is more than one line", r[1])
		}
		got.WriteString(r[0] + ": " + r[1] + "\n")
	}
	if !strings.HasPrefix(rows[0][0], "put ") || !strings.HasSuffix(rows[0][0], " wait") || !strings.HasPrefix(rows[0][1], "▰▰▰▰▰ ") || !strings.Contains(rows[0][1], " ms · p50 ") {
		t.Errorf("first row %q, want the rate limit wait with a full bar", rows[0])
	}
	for _, s := range []string{"whisper run: ▰▱▱▱▱", "· 1.5k", "go sched: 40 µs", "go gc pause: 300 µs"} {
		if !strings.Contains(got.String(), s) {
			t.Errorf("readout is missing %q:\n%s", s, got.String())
		}
	}
}

func TestEmptyReadout(t *testing.T) {
	if got, _ := module(obs.New()).readout(); got[0] != [2]string{"stages", "nothing measured yet"} {
		t.Fatalf("empty readout: %q", got)
	}
}

func TestPerfReplies(t *testing.T) {
	m := module(obs.New())
	if m.Name() != "perf" || m.Perms() != 0 || m.Want().Required != 0 || m.Help().Line == "" {
		t.Fatal("module surface changed")
	}
	c := m.Commands()
	if len(c) != 1 || c[0].Tier != core.BreakGlass {
		t.Fatal("/perf is not break-glass only")
	}
	e, sent := coretest.Event(t, "perf", nil)
	if err := c[0].Run(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	if len(*sent) != 1 || !(*sent)[0].Flags.Has(discord.MessageFlagEphemeral) || (*sent)[0].Embeds[0].Title != "perf" {
		t.Fatalf("sent %+v", *sent)
	}
}

// The numbers span every server, so a server's own admin, Administrator
// bit and all, is refused through the router.
func TestPerfIsBreakGlassOnly(t *testing.T) {
	r := core.NewRouter(core.Admins{9}, func(snowflake.ID) (snowflake.ID, bool) { return 5, true }, slog.New(slog.DiscardHandler))
	if err := r.Add(New(obs.New())); err != nil {
		t.Fatal(err)
	}
	e, sent := coretest.Event(t, "perf", func(p map[string]any) { p["member"].(map[string]any)["permissions"] = "8" })
	r.OnCommand(e)
	if len(*sent) != 1 || (*sent)[0].Content != "✗ only cygnet's keeper can use /perf" {
		t.Fatalf("a server admin and owner got %+v", *sent)
	}
	if New(obs.New()).Commands()[0].Create.(discord.SlashCommandCreate).DefaultMemberPermissions.Value == nil {
		t.Fatal("/perf shows in every member's picker")
	}
}

func TestFormatting(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0:                       "-",
		85 * time.Microsecond:   "85 µs",
		3100 * time.Microsecond: "3.1 ms",
		250 * time.Millisecond:  "250 ms",
		12 * time.Second:        "12s",
	} {
		if got := dur(d); got != want {
			t.Errorf("dur(%v) = %q, want %q", d, got, want)
		}
	}
	for n, want := range map[uint64]string{7: "7", 1500: "1.5k", 42000: "42k"} {
		if got := count(n); got != want {
			t.Errorf("count(%d) = %q, want %q", n, got, want)
		}
	}
	for _, c := range []struct {
		d, max time.Duration
		want   string
	}{
		{0, 0, "▱▱▱▱▱"},
		{100, 100, "▰▰▰▰▰"},
		{60, 100, "▰▰▰▱▱"},
		{1, 1000, "▰▱▱▱▱"},
	} {
		if got := bar(c.d, c.max); got != c.want {
			t.Errorf("bar(%v, %v) = %q, want %q", c.d, c.max, got, c.want)
		}
	}
}

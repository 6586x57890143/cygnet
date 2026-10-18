// Package perf is /perf: where cygnet spends her time, module by module and
// stage by stage, from the Go runtime up to Discord's API. It reads what
// internal/obs records and lays it out as a record, slowest first, each
// stage with a bar scaled to the slowest so the outlier shows at a glance.
//
// The numbers are the whole process's, every server cygnet is in, so they say
// how busy other servers keep her. /perf is BreakGlass: a server's own
// admins don't see them.
package perf

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/omit"

	"github.com/6586x57890143/cygnet/internal/brand"
	"github.com/6586x57890143/cygnet/internal/core"
	"github.com/6586x57890143/cygnet/internal/intents"
	"github.com/6586x57890143/cygnet/internal/obs"
)

var adminOnly = discord.PermissionAdministrator

// top is how many stages the readout shows.
const top = 12

type Module struct {
	rec     *obs.Recorder
	runtime func() (sched, gc time.Duration)
}

func New(rec *obs.Recorder) *Module { return &Module{rec: rec, runtime: obs.Runtime} }

func (*Module) Name() string { return "perf" }

func (*Module) Want() intents.Want { return intents.Want{} }

// Perms is none: /perf only replies to interactions.
func (*Module) Perms() discord.Permissions { return 0 }

// Help is perf's page in /help.
func (*Module) Help() core.Help {
	return core.Help{
		Color: brand.ColorInfo,
		Line:  "where her time goes with the slowest first",
		About: "/perf lists where her time goes in each module with the slowest first. every call is split four ways: in is the trip from discord's gateway to her, run is her own work, wait is time held back by discord's rate limits and http is the request itself. drop counts calls that gave up while waiting. the go runtime's own overhead is listed underneath. it covers every server she's in, so only her keeper can run it",
	}
}

func (m *Module) Commands() []core.Command {
	return []core.Command{{
		// Hidden from members in the picker; the tier is the real gate.
		Create: discord.SlashCommandCreate{Name: "perf", Description: "see where cygnet spends her time", DefaultMemberPermissions: omit.New(&adminOnly)},
		Tier:   core.BreakGlass,
		Run:    m.perf,
	}}
}

func (m *Module) perf(_ context.Context, e *events.ApplicationCommandInteractionCreate) error {
	embed, files := brand.Embed(brand.ColorInfo, "perf", "perf", "")
	embed.Fields = brand.Fields(m.readout())
	brand.Note(&embed, "since boot · in: gateway to dispatch · run: handler · wait: rate limit · http: round trip · build "+core.Revision())
	return e.CreateMessage(discord.MessageCreate{
		Embeds:          []discord.Embed{embed},
		Files:           files,
		Flags:           discord.MessageFlagEphemeral,
		AllowedMentions: core.NoPings(),
	})
}

// labelW keeps a stage's key short enough for a column.
const labelW = 16

// readout is a record's facts (DESIGN.md) in two sections. The stages,
// slowest first, each a line at full width: its p99 with a bar scaled to
// the slowest, its p50 and how many calls. Then the go runtime's own
// overhead, in columns.
func (m *Module) readout() (stages, runtime [][2]string) {
	rows := m.rec.Rows()
	if len(rows) == 0 {
		stages = append(stages, [2]string{"stages", "nothing measured yet"})
	}
	var slowest time.Duration
	if len(rows) > 0 {
		slowest = rows[0].P99
	}
	for _, r := range rows[:min(top, len(rows))] {
		label := clip(r.Who, labelW-1-len(r.Stage.String())) + " " + r.Stage.String()
		stages = append(stages, [2]string{label, bar(r.P99, slowest) + " " + dur(r.P99) + " · p50 " + dur(r.P50) + " · " + count(r.N)})
	}
	sched, gc := m.runtime()
	return stages, [][2]string{{"go sched", dur(sched)}, {"go gc pause", dur(gc)}}
}

// bar is d against max in five cells (DESIGN.md's bar), at least one for
// anything measured.
func bar(d, max time.Duration) string {
	const width = 5
	n := 0
	if max > 0 {
		n = int(int64(d) * width / int64(max))
	}
	if n == 0 && d > 0 {
		n = 1
	}
	return strings.Repeat("▰", n) + strings.Repeat("▱", width-n)
}

// dur is a duration the way UX.md writes numbers, down to microseconds.
func dur(d time.Duration) string {
	switch {
	case d <= 0:
		return "-"
	case d < time.Millisecond:
		return fmt.Sprintf("%d µs", d.Microseconds())
	case d < 10*time.Millisecond:
		return fmt.Sprintf("%.1f ms", float64(d)/float64(time.Millisecond))
	case d < 10*time.Second:
		return fmt.Sprintf("%d ms", d.Milliseconds())
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

func count(n uint64) string {
	switch {
	case n < 1000:
		return fmt.Sprint(n)
	case n < 10000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%dk", n/1000)
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

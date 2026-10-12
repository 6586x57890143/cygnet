package provision

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/brand"
	"github.com/6586x57890143/cygnet/internal/core"
)

const (
	previewID = "provision-preview"
	applyID   = "provision-apply"
	confirmID = "provision-confirm"
	// How a fresh start treats the channels it removes: deletes them, or
	// keeps them in the archive.
	replace = "replace"
	keepOld = "archive"
	// tick is how often the card is redrawn while a layout applies.
	tick = time.Second
)

// needs is what cygnet has to hold to apply l: roles and channels, and
// every permission l hands out.
func needs(l Layout) discord.Permissions {
	return discord.PermissionManageRoles | discord.PermissionManageChannels | l.Grants()
}

// planned is a layout planned against a server, ready to show or apply.
type planned struct {
	l       Layout
	s       state
	changes []change
	waiting []string
	// short is what cygnet still needs before she can apply it.
	short discord.Permissions
	self  snowflake.ID
	// fresh is a fresh start: what l doesn't name goes, once confirmed.
	fresh bool
}

// read reads guild as l is planned against it, and plans it; fresh, with
// whatever l doesn't name removed after.
func (m *Module) read(ctx context.Context, r rest.Rest, guild, self snowflake.ID, l Layout, fresh bool) (planned, error) {
	opts := rest.WithCtx(ctx)
	roles, err := r.GetRoles(guild, opts)
	if err != nil {
		return planned{}, err
	}
	channels, err := r.GetGuildChannels(guild, opts)
	if err != nil {
		return planned{}, err
	}
	me, err := r.GetMember(guild, self, opts)
	if err != nil {
		return planned{}, err
	}
	s := state{guild: guild, roles: roles, channels: channels, birds: m.birds(ctx)}
	s.birds[Self] = self
	for _, role := range roles {
		if role.ID == guild {
			continue
		}
		for _, id := range me.RoleIDs {
			if id == role.ID {
				s.top = max(s.top, role.Position)
			}
		}
	}
	if fresh {
		g, err := r.GetGuild(guild, false, opts)
		if err != nil {
			return planned{}, err
		}
		s.required = map[snowflake.ID]bool{}
		for _, id := range []*snowflake.ID{g.RulesChannelID, g.PublicUpdatesChannelID, g.SafetyAlertsChannelID} {
			if id != nil {
				s.required[*id] = true
			}
		}
	}
	p := planned{l: l, s: s, self: self, fresh: fresh, short: needs(l) &^ permsOf(guild, roles, me.RoleIDs)}
	p.changes, p.waiting = plan(l, s)
	if v, ok := m.kept.Load(guild); ok && !fresh {
		// What the admins left alone isn't planned; a fresh start starts
		// over and plans it all.
		leave := v.(Kept).Leave
		p.changes = slices.DeleteFunc(p.changes, func(c change) bool { return slices.Contains(leave, c.line()) })
	}
	if fresh {
		p.changes = append(p.changes, sweep(l, s)...)
	}
	return p, nil
}

// birds is each linked bird's application ID, from its manifest. A bird
// that doesn't answer is left out, so its overwrites wait.
func (m *Module) birds(ctx context.Context) map[string]snowflake.ID {
	out := map[string]snowflake.ID{}
	if m.fleet == nil {
		return out
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, b := range m.fleet.Fleet() {
		wg.Go(func() {
			r := row{Name: b.Name}
			r.manifest(ctx, m.fleet)
			if r.Err == "" {
				mu.Lock()
				out[b.Name] = r.App
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return out
}

// previewAgain redraws the preview from the server as it is now.
func (m *Module) previewAgain(ctx context.Context, e *events.ComponentInteractionCreate) error {
	l, err := m.pressed(e)
	if err != nil {
		return err
	}
	parts := strings.Split(e.Data.CustomID(), ":")
	p, err := m.read(ctx, e.Client().Rest, *e.GuildID(), e.ApplicationID(), l, len(parts) > 2 && parts[2] == "fresh")
	if err != nil {
		return fmt.Errorf("%w: %w", core.Tell("couldn't read this server; try again in a moment"), err)
	}
	return e.UpdateMessage(discord.NewMessageUpdateV2(p.preview("")).WithAllowedMentions(core.NoPings()))
}

// shown re-reads the server for the plan a button carries
// (id:layout:digest, and :replace or :archive in a fresh start), and says
// whether the server still matches it.
func (m *Module) shown(ctx context.Context, e *events.ComponentInteractionCreate) (p planned, how string, same bool, err error) {
	l, err := m.pressed(e)
	if err != nil {
		return p, "", false, err
	}
	parts := strings.Split(e.Data.CustomID(), ":")
	if len(parts) == 4 {
		how = parts[3]
	}
	p, err = m.read(ctx, e.Client().Rest, *e.GuildID(), e.ApplicationID(), l, how != "")
	if err != nil {
		return p, "", false, fmt.Errorf("%w: %w", core.Tell("couldn't read this server; try again in a moment"), err)
	}
	return p, how, (len(parts) == 3 || len(parts) == 4) && parts[2] == digest(p.changes) && p.short == 0, nil
}

func (m *Module) changed(e *events.ComponentInteractionCreate, p planned) error {
	return e.UpdateMessage(discord.NewMessageUpdateV2(p.preview("the server changed since that preview, so here's what it would do now")).WithAllowedMentions(core.NoPings()))
}

// confirmPressed asks before a fresh start deletes anything.
func (m *Module) confirmPressed(ctx context.Context, e *events.ComponentInteractionCreate) error {
	p, how, same, err := m.shown(ctx, e)
	if err != nil {
		return err
	}
	if !same || (how != replace && how != keepOld) {
		return m.changed(e, p)
	}
	return e.UpdateMessage(discord.NewMessageUpdateV2(p.confirm(how)).WithAllowedMentions(core.NoPings()))
}

// applyPressed applies the plan the card showed, and only that one: if the
// server has changed since, it shows the new plan instead.
func (m *Module) applyPressed(ctx context.Context, e *events.ComponentInteractionCreate) error {
	p, how, same, err := m.shown(ctx, e)
	if err != nil {
		return err
	}
	if !same {
		return m.changed(e, p)
	}
	if how == keepOld {
		p.changes = archivedTo(p.changes)
	}
	l := p.l
	guild := *e.GuildID()
	if _, busy := m.applying.LoadOrStore(guild, true); busy {
		return core.Tell("a layout is already being applied here; check again once it's done")
	}
	if err := e.DeferUpdateMessage(); err != nil {
		m.applying.Delete(guild)
		return err
	}
	// The writes outlive the response window and run off the gateway's
	// goroutine; the interaction's token is good for 15 minutes, far past
	// any layout.
	work := context.WithoutCancel(ctx)
	client, app, token := e.Client().Rest, e.ApplicationID(), e.Token()
	edit := func(card discord.ContainerComponent) error {
		_, err := client.UpdateInteractionResponse(app, token, discord.NewMessageUpdateV2(card).WithAllowedMentions(core.NoPings()), rest.WithCtx(work))
		return err
	}
	m.running.Go(func() {
		defer m.applying.Delete(guild)
		total := p.todo()
		last := time.Now()
		done, err := apply(work, client, m.guard, p.s, l, p.changes, func(done int) {
			if time.Since(last) >= tick && done < total {
				last = time.Now()
				_ = edit(p.progress(done, total))
			}
		})
		if err == nil {
			m.keep(work, guild, l.Name, p.fresh)
		}
		card := p.result(done, total, err)
		if edit(card) != nil && p.fresh {
			// A fresh start can take the channel the card was in with it,
			// so the result goes where staff will look.
			m.report(work, client, guild, l, card)
		}
	})
	return nil
}

// pressed is the layout a button names, for an admin in a server.
func (m *Module) pressed(e *events.ComponentInteractionCreate) (Layout, error) {
	if e.GuildID() == nil || !m.admin(e) {
		return Layout{}, core.Tell("only this server's admins can use /provision")
	}
	parts := strings.Split(e.Data.CustomID(), ":")
	if len(parts) < 2 {
		return Layout{}, core.Tell("that button is from an older cygnet; run /provision again")
	}
	l, ok := layout(parts[1])
	if !ok {
		return Layout{}, core.Tell("that layout isn't in this build of cygnet anymore")
	}
	return l, nil
}

// todo is how many changes apply will make.
func (p planned) todo() int {
	n := 0
	for _, c := range p.changes {
		if c.blocked == "" {
			n++
		}
	}
	return n
}

// preview is the plan as a card: a diff block of what changes, the
// overwrites spelled out below it, what's blocked or waiting, and apply.
func (p planned) preview(note string) discord.ContainerComponent {
	return withBack(p.card(note, true))
}

// card is the plan below note: with the buttons that grant cygnet what
// she's short of, apply it and check again, or as a report alone.
func (p planned) card(note string, act bool) discord.ContainerComponent {
	color := brand.ColorInfo
	var b strings.Builder
	if note != "" {
		b.WriteString("! " + note + "\n")
	}
	b.WriteString("-# " + p.l.About + "\n")
	todo := p.todo()
	if len(p.changes) == 0 {
		color = brand.ColorOK
		b.WriteString("✓ this server already matches **" + p.l.Name + "**")
	} else {
		fmt.Fprintf(&b, "▸ %s to make it match\n", count(todo, "change"))
		b.WriteString(p.diff())
		for _, c := range p.changes {
			if c.blocked != "" {
				color = brand.ColorWarn
				what := "change **" + c.role.Name + "**"
				switch c.kind {
				case roleOrder:
					what = "reorder the roles"
				case remove:
					what = "remove **" + cmp.Or(c.role.Name, c.name) + "**"
				}
				b.WriteString("\n! can't " + what + ": " + c.blocked)
			}
		}
	}
	for _, w := range p.waiting {
		b.WriteString("\n-# " + w)
	}
	var buttons []discord.InteractiveComponent
	if p.short != 0 {
		color = brand.ColorWarn
		b.WriteString("\n! cygnet needs " + names(p.short) + " first")
		buttons = append(buttons, discord.NewLinkButton("grant cygnet", install(p.self, needs(p.l), p.s.guild)))
	}
	if !act {
		return brand.Card(color, "provision", p.l.Name, b.String())
	}
	id, again := applyID+":"+p.l.Name+":"+digest(p.changes), previewID+":"+p.l.Name
	if p.fresh {
		id, again = id+":"+replace, again+":fresh"
	}
	chans, cats, roles := p.removals()
	switch {
	case chans+cats+roles > 0:
		// Nothing goes before the admin has said how, and then confirmed.
		b.WriteString("\n-# what's marked - goes. replace deletes it, messages and all; archive moves those channels into " + archiveName + ", hidden from everyone but admins, and deletes the rest")
		ask := confirmID + strings.TrimPrefix(id, applyID)
		archiveBtn := discord.NewPrimaryButton("archive old", strings.TrimSuffix(ask, replace)+keepOld)
		replaceBtn := discord.NewDangerButton("replace", ask)
		if p.short != 0 {
			archiveBtn, replaceBtn = archiveBtn.AsDisabled(), replaceBtn.AsDisabled()
		}
		if chans == 0 {
			// Nothing to archive: both would do the same.
			buttons = append(buttons, replaceBtn)
		} else {
			buttons = append(buttons, archiveBtn, replaceBtn)
		}
	default:
		applyBtn := discord.NewSuccessButton("apply "+count(todo, "change"), id)
		if todo == 0 || p.short != 0 {
			applyBtn = applyBtn.AsDisabled()
		}
		buttons = append(buttons, applyBtn)
	}
	buttons = append(buttons, discord.NewSecondaryButton("check again", again))
	return brand.Card(color, "provision", p.l.Name, b.String(),
		discord.NewSeparator(discord.SeparatorSpacingSizeSmall), discord.NewActionRow(buttons...))
}

// removals is how many channels, categories and roles a fresh start
// removes.
func (p planned) removals() (chans, cats, roles int) {
	for _, c := range p.changes {
		switch {
		case c.kind != remove || c.blocked != "":
		case c.role.Name != "":
			roles++
		case c.typ == discord.ChannelTypeGuildCategory:
			cats++
		default:
			chans++
		}
	}
	return chans, cats, roles
}

// confirm asks once more before a fresh start deletes anything, saying
// what goes for good, with the one button that does it.
func (p planned) confirm(how string) discord.ContainerComponent {
	chans, cats, roles := p.removals()
	others := p.todo() - chans - cats - roles
	var b strings.Builder
	b.WriteString("! this can't be undone\n")
	if how == keepOld {
		fmt.Fprintf(&b, "▸ cygnet moves %s into %s, hidden from everyone but admins\n", count(chans, "channel"), archiveName)
		chans = 0
	}
	var gone []string
	if chans > 0 {
		gone = append(gone, count(chans, "channel")+" with every message in them")
	}
	if cats == 1 {
		gone = append(gone, "1 category")
	} else if cats > 1 {
		gone = append(gone, fmt.Sprintf("%d categories", cats))
	}
	if roles > 0 {
		gone = append(gone, count(roles, "role"))
	}
	if len(gone) > 0 {
		b.WriteString("▸ cygnet deletes " + strings.Join(gone, ", ") + "\n")
	}
	if others > 0 {
		fmt.Fprintf(&b, "-# and makes the other %s the preview showed", count(others, "change"))
	}
	label := "delete and replace"
	if how == keepOld {
		label = "archive and replace"
	}
	return brand.Card(brand.ColorError, "provision", p.l.Name, b.String(),
		discord.NewSeparator(discord.SeparatorSpacingSizeSmall), discord.NewActionRow(
			discord.NewDangerButton(label, applyID+":"+p.l.Name+":"+digest(p.changes)+":"+how),
			discord.NewSecondaryButton("back", previewID+":"+p.l.Name+":fresh"),
		))
}

// diff is the changes as a diff block, then who may post where spelled
// out: new channels' overwrites and the ones changed on channels already
// here.
func (p planned) diff() string {
	var b strings.Builder
	b.WriteString("```diff\n")
	for _, c := range p.changes {
		if c.blocked == "" {
			b.WriteString(c.line() + "\n")
		}
	}
	b.WriteString("```")
	for _, c := range p.changes {
		switch c.kind {
		case channelCreate:
			for _, o := range c.channel.Overwrites {
				b.WriteString("\n-# #" + c.channel.Name + ": " + o.says())
			}
		case overwriteSet:
			b.WriteString("\n-# #" + c.channel.Name + ": " + c.ow.says())
		}
	}
	return b.String()
}

// progress is the card while a layout applies.
func (p planned) progress(done, total int) discord.ContainerComponent {
	return brand.Card(brand.ColorWarn, "provision", p.l.Name, fmt.Sprintf("▸ applying · %d of %d\n%s", done, total, bar(done, total)))
}

// result is the card once apply has finished or stopped.
func (p planned) result(done, total int, err error) discord.ContainerComponent {
	again := discord.NewActionRow(discord.NewSecondaryButton("check again", previewID+":"+p.l.Name))
	sep := discord.NewSeparator(discord.SeparatorSpacingSizeSmall)
	if err == nil {
		body := "✓ applied " + count(done, "change") + "; this server matches **" + p.l.Name + "**"
		for _, w := range p.waiting {
			body += "\n-# " + w
		}
		return withBack(brand.Card(brand.ColorOK, "provision", p.l.Name, body, sep, again))
	}
	why := refusal(err)
	if tell, ok := asTell(err); ok {
		why = tell
	}
	body := fmt.Sprintf("✗ stopped after %d of %d\n%s\n-# %s", done, total, bar(done, total), why)
	return withBack(brand.Card(brand.ColorError, "provision", p.l.Name, body, sep, again))
}

func asTell(err error) (string, bool) {
	t, ok := errors.AsType[core.Tell](err)
	return string(t), ok
}

// bar is a ten cell progress bar.
func bar(done, total int) string {
	n := 0
	if total > 0 {
		n = done * 10 / total
	}
	return strings.Repeat("▰", n) + strings.Repeat("▱", 10-n)
}

func count(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return fmt.Sprintf("%d %ss", n, thing)
}

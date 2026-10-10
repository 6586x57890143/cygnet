package provision

import (
	"context"
	"errors"
	"fmt"
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
}

// read reads guild as l is planned against it, and plans it.
func (m *Module) read(ctx context.Context, r rest.Rest, guild, self snowflake.ID, l Layout) (planned, error) {
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
	p := planned{l: l, s: s, self: self, short: needs(l) &^ permsOf(guild, roles, me.RoleIDs)}
	p.changes, p.waiting = plan(l, s)
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

func (m *Module) layoutCmd(ctx context.Context, e *events.ApplicationCommandInteractionCreate) error {
	l, ok := layout(e.SlashCommandInteractionData().String("name"))
	if !ok {
		l = layouts[0]
	}
	p, err := m.read(ctx, e.Client().Rest, *e.GuildID(), e.ApplicationID(), l)
	if err != nil {
		return fmt.Errorf("%w: %w", core.Tell("couldn't read this server; try again in a moment"), err)
	}
	return e.CreateMessage(discord.MessageCreate{
		Components:      []discord.LayoutComponent{p.preview("")},
		Flags:           discord.MessageFlagEphemeral | discord.MessageFlagIsComponentsV2,
		AllowedMentions: core.NoPings(),
	})
}

// previewAgain redraws the preview from the server as it is now.
func (m *Module) previewAgain(ctx context.Context, e *events.ComponentInteractionCreate) error {
	l, err := m.pressed(e)
	if err != nil {
		return err
	}
	p, err := m.read(ctx, e.Client().Rest, *e.GuildID(), e.ApplicationID(), l)
	if err != nil {
		return fmt.Errorf("%w: %w", core.Tell("couldn't read this server; try again in a moment"), err)
	}
	return e.UpdateMessage(discord.NewMessageUpdateV2(p.preview("")).WithAllowedMentions(core.NoPings()))
}

// applyPressed applies the plan the card showed, and only that one: if the
// server has changed since, it shows the new plan instead.
func (m *Module) applyPressed(ctx context.Context, e *events.ComponentInteractionCreate) error {
	l, err := m.pressed(e)
	if err != nil {
		return err
	}
	parts := strings.Split(e.Data.CustomID(), ":")
	p, err := m.read(ctx, e.Client().Rest, *e.GuildID(), e.ApplicationID(), l)
	if err != nil {
		return fmt.Errorf("%w: %w", core.Tell("couldn't read this server; try again in a moment"), err)
	}
	if len(parts) != 3 || parts[2] != digest(p.changes) || p.short != 0 {
		return e.UpdateMessage(discord.NewMessageUpdateV2(p.preview("the server changed since that preview, so here's what it would do now")).WithAllowedMentions(core.NoPings()))
	}
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
	edit := func(card discord.ContainerComponent) {
		_, _ = client.UpdateInteractionResponse(app, token, discord.NewMessageUpdateV2(card).WithAllowedMentions(core.NoPings()), rest.WithCtx(work))
	}
	m.running.Go(func() {
		defer m.applying.Delete(guild)
		total := p.todo()
		last := time.Now()
		done, err := apply(work, client, m.guard, p.s, l, p.changes, func(done int) {
			if time.Since(last) >= tick && done < total {
				last = time.Now()
				edit(p.progress(done, total))
			}
		})
		edit(p.result(done, total, err))
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
		return Layout{}, core.Tell("that button is from an older cygnet; run /provision layout again")
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
		fmt.Fprintf(&b, "▸ %s to make it match\n```diff\n", count(todo, "change"))
		for _, c := range p.changes {
			if c.blocked == "" {
				b.WriteString(c.line() + "\n")
			}
		}
		b.WriteString("```")
		// Who may post where, spelled out: new channels' overwrites and
		// the ones changed on channels already here.
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
		for _, c := range p.changes {
			if c.blocked != "" {
				color = brand.ColorWarn
				b.WriteString("\n! can't change **" + c.role.Name + "**: " + c.blocked)
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
	applyBtn := discord.NewSuccessButton("apply "+count(todo, "change"), applyID+":"+p.l.Name+":"+digest(p.changes))
	if todo == 0 || p.short != 0 {
		applyBtn = applyBtn.AsDisabled()
	}
	buttons = append(buttons, applyBtn, discord.NewSecondaryButton("check again", previewID+":"+p.l.Name))
	return brand.Card(color, "provision", p.l.Name, b.String(),
		discord.NewSeparator(discord.SeparatorSpacingSizeSmall), discord.NewActionRow(buttons...))
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
		return brand.Card(brand.ColorOK, "provision", p.l.Name, body, sep, again)
	}
	why := refusal(err)
	if tell, ok := asTell(err); ok {
		why = tell
	}
	body := fmt.Sprintf("✗ stopped after %d of %d\n%s\n-# %s", done, total, bar(done, total), why)
	return brand.Card(brand.ColorError, "provision", p.l.Name, body, sep, again)
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

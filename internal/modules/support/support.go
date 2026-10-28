// Package support is backing the birds (docs/support.md): a monthly
// supporter, paid in crypto into wallets only the owner holds, for a
// length bought up front. cygnet.lol's Worker sells it; this module
// quotes each checkout an amount nobody else's has, watches the chains for
// it (internal/chain) and records what it bought (store.Support), and
// answers the birds who backs them, so each gives its supporter role. The
// command shows anyone their own backing, and its keeper the rest.
package support

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/brand"
	"github.com/6586x57890143/cygnet/internal/chain"
	"github.com/6586x57890143/cygnet/internal/core"
	"github.com/6586x57890143/cygnet/internal/intents"
	"github.com/6586x57890143/cygnet/internal/store"
)

// Wallets is where payments go, by address family: one 0x address takes
// every EVM rail. A family left empty offers none of its rails.
type Wallets struct {
	EVM, Tron, Solana string
}

func (w Wallets) of(f chain.Family) string {
	switch f {
	case chain.EVM:
		return w.EVM
	case chain.Tron:
		return w.Tron
	case chain.Solana:
		return w.Solana
	}
	return ""
}

// Check refuses a wallet that isn't an address of its family, so a typo
// fails boot instead of publishing somewhere nobody can spend from.
func (w Wallets) Check() error {
	for _, f := range []chain.Family{chain.EVM, chain.Tron, chain.Solana} {
		if a := w.of(f); a != "" && !chain.Valid(f, a) {
			return fmt.Errorf("the %s wallet %q is not a %s address", f, a, f)
		}
	}
	return nil
}

// The panel's custom IDs: "support" redraws it, "support:gift:<kind>"
// opens the gift form, and "support-gift:<kind>" is the form, where kind
// is plain or gold.
const (
	id     = "support"
	giftID = "support-gift"
)

type Module struct {
	// store is nil without a database.
	store   *store.Support
	reader  *chain.Reader
	wallets Wallets
	// owner is who the money goes to, and profile reads their name and
	// picture for the Worker's page.
	owner   snowflake.ID
	profile func(context.Context, snowflake.ID) (Owner, error)
	// keepers is cygnet's bootstrap admins: who see the takings and can
	// give months.
	keepers []snowflake.ID
	// page is the checkout at cygnet.lol, "" when there is none.
	page string
	log  *slog.Logger
	now  func() time.Time

	watching sync.Once
	// rounds counts reads, to turn the order Solana accounts are read in.
	rounds atomic.Int64
	// prices is the spot prices quotes share (verbs.go).
	prices struct {
		sync.Mutex
		got map[string]spot
	}
	cache struct {
		sync.Mutex
		owner   Owner
		at      time.Time
		reading bool
	}
}

// New is the module. s is nil without a database; wallets have been
// Checked.
func New(s *store.Support, r *chain.Reader, wallets Wallets, owner snowflake.ID, keepers []snowflake.ID, page string, profile func(context.Context, snowflake.ID) (Owner, error), log *slog.Logger) *Module {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Module{store: s, reader: r, wallets: wallets, owner: owner, keepers: keepers, page: page, profile: profile, log: log, now: time.Now}
}

func (*Module) Name() string { return "support" }

func (*Module) Want() intents.Want { return intents.Want{Required: gateway.IntentGuilds} }

// Perms is none: the panel only replies.
func (*Module) Perms() discord.Permissions { return 0 }

func (*Module) Help() core.Help {
	return core.Help{
		Color: brand.ColorPrimary,
		Line:  "backing the birds, paid in crypto",
		About: "/support shows whether you back the birds and until when, and the way to start or add to it at cygnet.lol. backing is bought up front for a month or more and paid in usdc, usdt or sol into a wallet only its owner holds. cygnet asks for an exact amount, sees it arrive on the chain and counts it, and the birds give their supporter role to whoever backs them. nothing renews by itself",
	}
}

func (m *Module) Commands() []core.Command {
	return []core.Command{{
		Create: discord.SlashCommandCreate{
			Name:        "support",
			Description: "whether you back the birds, and the way to",
			Contexts:    []discord.InteractionContextType{discord.InteractionContextTypeGuild},
		},
		Tier: core.Public,
		Run:  m.open,
	}}
}

func (m *Module) Components() []core.Component {
	return []core.Component{{ID: id, Run: m.click}}
}

func (m *Module) Modals() []core.Modal {
	return []core.Modal{{ID: giftID, Run: m.gifted}}
}

// OnEvent starts reading the chains once cygnet is connected.
func (m *Module) OnEvent(ev bot.Event) {
	if _, ok := ev.(*events.Ready); ok && m.store != nil && len(m.rails()) > 0 {
		m.watching.Do(func() { go m.watch(context.Background()) })
	}
}

// rails is what a checkout can be paid in: every rail whose wallet is set.
func (m *Module) rails() []chain.Rail {
	var out []chain.Rail
	for _, r := range chain.Rails {
		if m.wallets.of(r.Family) != "" && (!r.Native || (m.reader != nil && m.reader.Native[r.Chain] != "")) {
			out = append(out, r)
		}
	}
	return out
}

func (m *Module) rail(key string) (chain.Rail, bool) {
	r, ok := chain.Find(key)
	return r, ok && m.wallets.of(r.Family) != "" && (!r.Native || (m.reader != nil && m.reader.Native[r.Chain] != ""))
}

// ownerProfile is who the money goes to, their name and picture read at
// most hourly; just their ID if Discord can't say. One page view reads
// Discord at a time, outside the lock, and the rest are answered from the
// cache meanwhile. A failed read keeps the name read before and is retried
// in five minutes, so a Discord outage costs one call every few minutes
// rather than one a page view.
func (m *Module) ownerProfile(ctx context.Context) Owner {
	bare := Owner{ID: m.owner.String()}
	m.cache.Lock()
	if (m.cache.owner.ID != "" && m.now().Sub(m.cache.at) < time.Hour) || m.cache.reading {
		o := m.cache.owner
		m.cache.Unlock()
		if o.ID == "" {
			return bare
		}
		return o
	}
	m.cache.reading = true
	m.cache.Unlock()

	o, err := bare, error(nil)
	if m.profile != nil {
		if o, err = m.profile(ctx, m.owner); err != nil {
			m.log.Warn("reading the owner's profile", "err", err)
		}
		o.ID = bare.ID
	}

	m.cache.Lock()
	defer m.cache.Unlock()
	m.cache.reading = false
	if err != nil {
		o = bare
		if m.cache.owner.ID != "" {
			o = m.cache.owner
		}
		m.cache.owner, m.cache.at = o, m.now().Add(-time.Hour+5*time.Minute)
		return o
	}
	m.cache.owner, m.cache.at = o, m.now()
	return o
}

var errNoStore = core.Tell("cygnet keeps supporters in her database; set CYGNET_DATABASE_URL")

func (m *Module) open(ctx context.Context, e *events.ApplicationCommandInteractionCreate) error {
	if m.store == nil {
		return errNoStore
	}
	card, err := m.panel(ctx, e.User().ID, "")
	if err != nil {
		return err
	}
	return e.CreateMessage(discord.MessageCreate{
		Components:      []discord.LayoutComponent{card},
		Flags:           discord.MessageFlagEphemeral | discord.MessageFlagIsComponentsV2,
		AllowedMentions: core.NoPings(),
	})
}

// click runs a button: the gift form for the keeper, or a fresh look.
func (m *Module) click(ctx context.Context, e *events.ComponentInteractionCreate) error {
	if m.store == nil {
		return errNoStore
	}
	parts := strings.Split(e.Data.CustomID(), ":")
	if len(parts) == 3 && parts[1] == "gift" {
		if !m.isKeeper(e.User().ID) {
			return core.Tell("only cygnet's keeper gives backing")
		}
		return e.Modal(giftForm(parts[2] == "gold"))
	}
	card, err := m.panel(ctx, e.User().ID, "")
	if err != nil {
		return err
	}
	return e.UpdateMessage(discord.NewMessageUpdateV2(card).WithAllowedMentions(core.NoPings()))
}

func (m *Module) isKeeper(user snowflake.ID) bool {
	return user != 0 && slices.Contains(m.keepers, user)
}

// giftForm asks whom to give backing and for how long.
func giftForm(gold bool) discord.ModalCreate {
	kind, title := "plain", "give backing"
	if gold {
		kind, title = "gold", "give gold backing"
	}
	return discord.ModalCreate{
		CustomID: giftID + ":" + kind,
		Title:    title,
		Components: []discord.LayoutComponent{
			discord.NewLabel("their discord id", discord.TextInputComponent{CustomID: "user", Style: discord.TextInputStyleShort, Required: true, MinLength: new(17), MaxLength: 20}),
			discord.NewLabel("months", discord.TextInputComponent{CustomID: "months", Style: discord.TextInputStyleShort, Required: true, MaxLength: 2, Placeholder: "1 to 36"}),
		},
	}
}

// gifted gives the backing the form names, after whatever they have, and
// redraws the panel.
func (m *Module) gifted(ctx context.Context, e *events.ModalSubmitInteractionCreate) error {
	if !m.isKeeper(e.User().ID) {
		return core.Tell("only cygnet's keeper gives backing")
	}
	if m.store == nil {
		return errNoStore
	}
	user, err := snowflake.Parse(strings.TrimSpace(e.Data.Text("user")))
	if err != nil || user == 0 {
		return core.Tell("that isn't a discord id; copy it from their profile with developer mode on")
	}
	n, err := strconv.Atoi(strings.TrimSpace(e.Data.Text("months")))
	if err != nil || n < 1 || n > 36 {
		return core.Tell("months is a number from 1 to 36")
	}
	gold := strings.HasSuffix(e.Data.CustomID, ":gold")
	p, err := m.store.Gift(ctx, user, n, gold, e.User().ID, m.now())
	if errors.Is(err, store.ErrNotOffered) {
		return core.Tell("months is a number from 1 to 36")
	}
	if err != nil {
		return err
	}
	what := "backing"
	if gold {
		what = "gold backing"
	}
	card, err := m.panel(ctx, e.User().ID, fmt.Sprintf("✓ gave <@%s> %s of %s, until %s", user, months(n), what, day(p.Ends)))
	if err != nil {
		return err
	}
	return e.UpdateMessage(discord.NewMessageUpdateV2(card).WithAllowedMentions(core.NoPings()))
}

// panel is someone's own backing and the way to it, and for the keeper
// what has come in, how the chains read and what matched nothing.
func (m *Module) panel(ctx context.Context, user snowflake.ID, note string) (discord.ContainerComponent, error) {
	now := m.now()
	st, err := m.store.Status(ctx, user, now)
	if err != nil {
		return discord.ContainerComponent{}, err
	}
	var b strings.Builder
	if note != "" {
		b.WriteString(note + "\n")
	}
	switch {
	case st.Active && st.Gold:
		fmt.Fprintf(&b, "**backing** gold, until %s\n-# since %s; thank you", day(st.Until), day(st.Since))
	case st.Active:
		fmt.Fprintf(&b, "**backing** until %s\n-# since %s; thank you", day(st.Until), day(st.Since))
	case !st.Until.IsZero():
		fmt.Fprintf(&b, "**backing** ran out %s\n-# you can pick it back up whenever you like", day(st.Until))
	default:
		b.WriteString("**backing** not yet\n-# it keeps the birds running and goes to the person who builds them")
	}
	color := brand.ColorInfo
	if st.Active {
		color = brand.ColorOK
	}
	sep := discord.NewSeparator(discord.SeparatorSpacingSizeSmall)
	var extra []discord.ContainerSubComponent
	if m.page != "" {
		label := "back the birds"
		if st.Active {
			label = "add to it"
		}
		extra = append(extra, sep, discord.NewActionRow(discord.NewLinkButton(label, m.page)))
	}
	if m.isKeeper(user) {
		keep, err := m.keeperView(ctx, now)
		if err != nil {
			return discord.ContainerComponent{}, err
		}
		extra = append(extra, sep, discord.NewTextDisplay(keep),
			discord.NewActionRow(
				discord.NewSecondaryButton("give backing", id+":gift:plain"),
				discord.NewSecondaryButton("give gold backing", id+":gift:gold"),
				discord.NewSecondaryButton("check again", id),
			))
	}
	return brand.Card(color, "support", "", b.String(), extra...), nil
}

// keeperView is the takings: who backs the birds, what came in on each
// rail over 30 days, how each rail last read, and what matched nothing.
func (m *Module) keeperView(ctx context.Context, now time.Time) (string, error) {
	all, err := m.store.Supporters(ctx, now)
	if err != nil {
		return "", err
	}
	gold := 0
	for _, s := range all {
		if s.Gold {
			gold++
		}
	}
	got, err := m.store.Received(ctx, now.Add(-30*24*time.Hour))
	if err != nil {
		return "", err
	}
	cursors, err := m.store.Cursors(ctx)
	if err != nil {
		return "", err
	}
	unmatched, err := m.store.Unmatched(ctx, 3)
	if err != nil {
		return "", err
	}
	rows := [][2]string{{"supporters", fmt.Sprintf("%d · %d gold", len(all), gold)}}
	var in []string
	for _, r := range chain.Rails {
		if v := got[r.Key]; v != nil && v.Sign() > 0 {
			in = append(in, r.Format(v)+" "+r.Label)
		}
	}
	if len(in) == 0 {
		in = []string{"nothing yet"}
	}
	rows = append(rows, [2]string{"30 days", strings.Join(in, " · ")})
	var reads []string
	for _, r := range m.rails() {
		reads = append(reads, r.Label+" "+health(cursors, r.Key, now))
	}
	if len(reads) == 0 {
		reads = []string{"no wallet set"}
	}
	rows = append(rows, [2]string{"chains", strings.Join(reads, " · ")})
	out := core.Readout(rows)
	for _, p := range unmatched {
		ago := core.Duration(now.Sub(p.Seen))
		// A rail since dropped from the table still has its payments:
		// they read as raw units on their key, with no explorer to link.
		r, ok := chain.Find(p.Rail)
		if !ok {
			out += fmt.Sprintf("-# ! %s units on %s matched no checkout, %s ago\n", p.Amount, p.Rail, ago)
			continue
		}
		out += fmt.Sprintf("-# ! %s %s matched no checkout, %s ago: [the transfer](%s)\n", r.Format(p.Amount), r.Label, ago, fmt.Sprintf(r.Tx, p.Tx))
	}
	return strings.TrimSuffix(out, "\n"), nil
}

// health is how reading a rail last went: when it last read, or why it
// failed, across its cursors (one per Solana account).
func health(cursors []store.Cursor, rail string, now time.Time) string {
	var last time.Time
	var failed string
	for _, c := range cursors {
		if c.Key != rail && !strings.HasPrefix(c.Key, rail+":") {
			continue
		}
		if c.Err != "" && failed == "" {
			failed = c.Err
		}
		if c.Read.After(last) {
			last = c.Read
		}
	}
	switch {
	case failed != "":
		return "✗ " + clip(failed, 60)
	case last.IsZero():
		return "not read yet"
	}
	return "✓ " + core.Duration(now.Sub(last)) + " ago"
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-3]) + "..."
	}
	return s
}

// day is a date as cygnet writes one: "12 jan 2027".
func day(t time.Time) string { return strings.ToLower(t.UTC().Format("2 Jan 2006")) }

// months is n months, or 1 month.
func months(n int) string {
	if n == 1 {
		return "1 month"
	}
	return strconv.Itoa(n) + " months"
}

// Offered is the rails a checkout can be paid in, for main's log line.
func (m *Module) Offered() []string {
	var out []string
	for _, r := range m.rails() {
		out = append(out, r.Key)
	}
	return slices.Clip(out)
}

package launch

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/store"
	"github.com/6586x57890143/cygnet/link"
)

// What cygnet.lol's Worker asks about a launch, over POST /v1/call as the
// bird edge (docs/edge.md). cygnet keeps the line, its invites and the
// ledger; the Worker keeps what stops one person signing up twice and the
// proofs of their email, and asks her for the rest. Each verb that changes
// something is safe to send again: joining or vouching twice is refused or
// returns what the first did.

// edgeBird is the name cygnet.lol's Worker links to the hub as: the only
// caller these verbs answer.
const edgeBird = "edge"

// inviteLife is how long an invite stays good once it's made.
const inviteLife = 48 * time.Hour

// Why a verb is refused, as a denied error's reason, for the Worker to say
// in its own words.
const (
	NoLaunch  = "no_launch"
	Closed    = "closed"
	OnList    = "on_list"
	NotOnList = "not_on_list"
	NoneLeft  = "none_left"
	Gone      = "gone"
	Own       = "own"
	NotYours  = "not_yours"
)

// Ask is every verb's body: the launch, who signed in, and, as the verb
// needs, an invite's code (its SHA-256, in hex), why their invites are
// held, and who a lift is for.
type Ask struct {
	Launch string `json:"launch,omitempty"`
	User   string `json:"user"`
	Code   string `json:"code,omitempty"`
	Held   string `json:"held,omitempty"`
	Member string `json:"member,omitempty"`
}

// Invite is one invite someone made, as their page shows it: times are
// Unix milliseconds, 0 for not yet.
type Invite struct {
	Code    string `json:"code"`
	Made    int64  `json:"made"`
	Expires int64  `json:"expires"`
	SpentAt int64  `json:"spent_at,omitempty"`
	Revoked int64  `json:"revoked,omitempty"`
}

// View is cygnet.place's answer: everything a person's page shows.
type View struct {
	Place     int      `json:"place"`
	Rank      int      `json:"rank"`
	Sponsored bool     `json:"sponsored"`
	Held      string   `json:"held,omitempty"`
	Released  int      `json:"released"`
	Left      int      `json:"left"`
	Sealed    bool     `json:"sealed"`
	Admitted  int64    `json:"admitted,omitempty"`
	Invites   []Invite `json:"invites"`
}

// Row is one person on a launch's list, for cygnet.list. Sponsor is who
// brought them in: "" for nobody, "gone" for someone who left.
type Row struct {
	User     string `json:"user"`
	Sponsor  string `json:"sponsor,omitempty"`
	Place    int    `json:"place"`
	Released int    `json:"released"`
	Held     string `json:"held,omitempty"`
	At       int64  `json:"at"`
	Admitted int64  `json:"admitted,omitempty"`
}

// Verbs is every verb this module answers, by name, for main to serve on
// the hub.
func (m *Module) Verbs() map[string]func(context.Context, string, json.RawMessage) (any, error) {
	return map[string]func(context.Context, string, json.RawMessage) (any, error){
		"join": m.join, "place": m.place, "issue": m.issue, "revoke": m.revoke, "vouch": m.vouch,
		"leave": m.leave, "mine": m.mine, "list": m.list, "lift": m.lift,
	}
}

// read checks a call is the Worker's and decodes its body, and who signed
// in.
func (m *Module) read(caller string, body json.RawMessage) (Ask, snowflake.ID, error) {
	var a Ask
	if caller != edgeBird {
		return a, 0, link.Err(link.Denied, "only cygnet.lol asks about launches")
	}
	if m.store == nil {
		return a, 0, link.Err(link.Denied, "cygnet keeps no launches without a database")
	}
	if err := json.Unmarshal(body, &a); err != nil {
		return a, 0, link.Err(link.Invalid, "body")
	}
	user, err := snowflake.Parse(a.User)
	if err != nil {
		return a, 0, link.Err(link.Invalid, "user is a Discord ID")
	}
	return a, user, nil
}

// code is a's invite code hash.
func (a Ask) code() ([]byte, error) {
	h, err := hex.DecodeString(a.Code)
	if err != nil || len(h) != 32 {
		return nil, link.Err(link.Invalid, "code is a SHA-256 in hex")
	}
	return h, nil
}

// refused is a store refusal as the Worker reads it.
func refused(err error) error {
	for _, r := range []struct {
		err error
		why string
	}{
		{store.ErrNoLaunch, NoLaunch}, {store.ErrClosed, Closed}, {store.ErrOnList, OnList},
		{store.ErrNotOnList, NotOnList}, {store.ErrNoneLeft, NoneLeft}, {store.ErrGone, Gone}, {store.ErrOwn, Own},
	} {
		if errors.Is(err, r.err) {
			return link.Err(link.Denied, r.why)
		}
	}
	return err
}

func ms(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

// join is cygnet.join: user at the back of the line, held for why if the
// Worker saw someone like them on it.
func (m *Module) join(ctx context.Context, caller string, body json.RawMessage) (any, error) {
	a, user, err := m.read(caller, body)
	if err != nil {
		return nil, err
	}
	place, err := m.store.Join(ctx, a.Launch, user, a.Held, nil, m.now())
	if err != nil {
		return nil, refused(err)
	}
	return map[string]int{"place": place}, nil
}

// place is cygnet.place: where user stands, for their page.
func (m *Module) place(ctx context.Context, caller string, body json.RawMessage) (any, error) {
	a, user, err := m.read(caller, body)
	if err != nil {
		return nil, err
	}
	p, err := m.store.Where(ctx, a.Launch, user, m.now())
	if err != nil {
		return nil, refused(err)
	}
	v := View{Place: p.Place, Rank: p.Rank, Sponsored: p.Sponsor != 0, Held: p.Held, Released: p.Released, Left: p.Left, Sealed: p.Sealed, Admitted: ms(p.Admitted), Invites: []Invite{}}
	for _, c := range p.Invites {
		v.Invites = append(v.Invites, Invite{Code: hex.EncodeToString(c.Hash), Made: ms(c.Made), Expires: ms(c.Expires), SpentAt: ms(c.SpentAt), Revoked: ms(c.Revoked)})
	}
	return v, nil
}

// issue is cygnet.issue: keep the invite the Worker made for user, by its
// code's hash. The code itself stays with the Worker and the link.
func (m *Module) issue(ctx context.Context, caller string, body json.RawMessage) (any, error) {
	a, user, err := m.read(caller, body)
	if err != nil {
		return nil, err
	}
	h, err := a.code()
	if err != nil {
		return nil, err
	}
	now := m.now()
	if err := m.store.Issue(ctx, a.Launch, h, user, now, now.Add(inviteLife)); err != nil {
		return nil, refused(err)
	}
	return map[string]int64{"expires": ms(now.Add(inviteLife))}, nil
}

// revoke is cygnet.revoke: take back an invite of user's nobody used.
func (m *Module) revoke(ctx context.Context, caller string, body json.RawMessage) (any, error) {
	a, user, err := m.read(caller, body)
	if err != nil {
		return nil, err
	}
	h, err := a.code()
	if err != nil {
		return nil, err
	}
	done, err := m.store.Revoke(ctx, a.Launch, h, user, m.now())
	if err != nil {
		return nil, err
	}
	return map[string]bool{"revoked": done}, nil
}

// vouch is cygnet.vouch: spend an invite for user and put them on its
// launch's line under whoever gave it. Sent again, it answers the same.
func (m *Module) vouch(ctx context.Context, caller string, body json.RawMessage) (any, error) {
	a, user, err := m.read(caller, body)
	if err != nil {
		return nil, err
	}
	h, err := a.code()
	if err != nil {
		return nil, err
	}
	name, err := m.store.Vouch(ctx, h, user, a.Held, m.now())
	if err != nil {
		return nil, refused(err)
	}
	return map[string]string{"launch": name}, nil
}

// leave is cygnet.leave: user off the list, and forgotten in its record.
func (m *Module) leave(ctx context.Context, caller string, body json.RawMessage) (any, error) {
	a, user, err := m.read(caller, body)
	if err != nil {
		return nil, err
	}
	return map[string]bool{}, m.store.Leave(ctx, a.Launch, user, m.now())
}

// mine is cygnet.mine: everything cygnet keeps about user in a launch, for
// them to download.
func (m *Module) mine(ctx context.Context, caller string, body json.RawMessage) (any, error) {
	a, user, err := m.read(caller, body)
	if err != nil {
		return nil, err
	}
	p, err := m.store.Where(ctx, a.Launch, user, m.now())
	if err != nil {
		return nil, refused(err)
	}
	marks, err := m.store.History(ctx, a.Launch, user)
	if err != nil {
		return nil, err
	}
	when := func(t time.Time) any {
		if t.IsZero() {
			return nil
		}
		return t.UTC().Format(time.RFC3339)
	}
	invites := []map[string]any{}
	for _, c := range p.Invites {
		invites = append(invites, map[string]any{"made": when(c.Made), "expires": when(c.Expires), "used": when(c.SpentAt), "taken_back": when(c.Revoked)})
	}
	ledger := []map[string]any{}
	for _, mk := range marks {
		ledger = append(ledger, map[string]any{"what": mk.Kind, "by_you": mk.ByYou, "at": when(mk.At)})
	}
	return map[string]any{
		"launch": a.Launch, "discord_id": user.String(), "place": p.Place, "invited_by_someone": p.Sponsor != 0,
		"invites_released": p.Released, "on_hold_because": nullable(p.Held), "add_me_when_it_opens": p.Sealed,
		"let_in": when(p.Admitted), "invites_made": invites, "ledger": ledger, "ledger_id": hex.EncodeToString(m.store.Who(user)),
	}, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// mayRun is whether user may see and change a launch's list from the web:
// its server's owner, or the breakglass admin.
func (m *Module) mayRun(ctx context.Context, name string, user snowflake.ID) error {
	l, err := m.store.Get(ctx, name)
	if err != nil {
		return refused(err)
	}
	g, ok := m.guild(l.Guild)
	if !ok || m.edge == nil || !slices.Contains(m.edge.admins(g), user) {
		return link.Err(link.Denied, NotYours)
	}
	return nil
}

// list is cygnet.list: everyone on a launch's list, for whoever runs it.
func (m *Module) list(ctx context.Context, caller string, body json.RawMessage) (any, error) {
	a, user, err := m.read(caller, body)
	if err != nil {
		return nil, err
	}
	if err := m.mayRun(ctx, a.Launch, user); err != nil {
		return nil, err
	}
	rows, err := m.store.List(ctx, a.Launch)
	if err != nil {
		return nil, err
	}
	out := []Row{}
	for _, r := range rows {
		row := Row{User: r.User.String(), Place: r.Place, Released: r.Released, Held: r.Held, At: ms(r.At), Admitted: ms(r.Admitted)}
		switch {
		case store.Forgotten(r.Sponsor):
			row.Sponsor = "gone"
		case r.Sponsor != 0:
			row.Sponsor = r.Sponsor.String()
		}
		out = append(out, row)
	}
	return map[string]any{"rows": out}, nil
}

// lift is cygnet.lift: whoever runs a launch lets a held member's invites
// be released.
func (m *Module) lift(ctx context.Context, caller string, body json.RawMessage) (any, error) {
	a, user, err := m.read(caller, body)
	if err != nil {
		return nil, err
	}
	if err := m.mayRun(ctx, a.Launch, user); err != nil {
		return nil, err
	}
	member, err := snowflake.Parse(a.Member)
	if err != nil {
		return nil, link.Err(link.Invalid, "member is a Discord ID")
	}
	lifted, err := m.store.Lift(ctx, a.Launch, member, user, m.now())
	if err != nil {
		return nil, err
	}
	return map[string]bool{"lifted": lifted}, nil
}

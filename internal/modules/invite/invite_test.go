package invite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/core/coretest"
	"github.com/6586x57890143/cygnet/internal/guard"
	"github.com/6586x57890143/cygnet/internal/store"
)

// memory is the store as a test keeps it, with Codes' rules.
type memory struct {
	mu      sync.Mutex
	codes   []store.Code
	each    map[snowflake.ID]int
	fail    error
	spends  int
	tree    map[snowflake.ID][]store.Join
	arrived []string
	left    []string
}

func (s *memory) counted(guild, owner snowflake.ID, now time.Time) int {
	n := 0
	for _, c := range s.codes {
		if c.Guild == guild && c.Owner == owner && c.Revoked.IsZero() && (c.SpentBy != 0 || now.Before(c.Expires)) {
			n++
		}
	}
	return n
}

func (s *memory) Mint(_ context.Context, hash []byte, guild, owner snowflake.ID, now, expires time.Time, limit int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit >= 0 && s.counted(guild, owner, now) >= limit {
		return store.ErrNoneLeft
	}
	s.codes = append([]store.Code{{Hash: hash, Guild: guild, Owner: owner, Made: now, Expires: expires}}, s.codes...)
	return nil
}

func (s *memory) Left(_ context.Context, guild, owner snowflake.ID, now time.Time, limit int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit < 0 {
		return -1, nil
	}
	return max(0, limit-s.counted(guild, owner, now)), s.fail
}

func (s *memory) Mine(_ context.Context, guild, owner snowflake.ID, n int) ([]store.Code, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.Code
	for _, c := range s.codes {
		if c.Guild == guild && c.Owner == owner && len(out) < n {
			out = append(out, c)
		}
	}
	return out, nil
}

func (s *memory) Revoke(_ context.Context, hash []byte, owner snowflake.ID, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.codes {
		if bytes.Equal(c.Hash, hash) && c.Owner == owner && c.SpentBy == 0 && c.Revoked.IsZero() {
			s.codes[i].Revoked = now
			return true, nil
		}
	}
	return false, nil
}

func (s *memory) Each(_ context.Context, guild snowflake.ID) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.each[guild], s.fail
}

func (s *memory) Find(_ context.Context, hash []byte) (store.Code, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.codes {
		if bytes.Equal(c.Hash, hash) {
			return c, nil
		}
	}
	return store.Code{}, store.ErrGone
}

// Spend is Codes.Spend's rules but for the one way in, which the store's
// own tests hold.
func (s *memory) Spend(_ context.Context, hash []byte, user snowflake.ID, now time.Time) (store.Code, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spends++
	for i, c := range s.codes {
		switch {
		case !bytes.Equal(c.Hash, hash):
		case c.SpentBy == user:
			return c, nil
		case c.Owner == user:
			return store.Code{}, store.ErrOwn
		case c.Open(now):
			s.codes[i].SpentBy, s.codes[i].SpentAt = user, now
			return s.codes[i], nil
		}
	}
	return store.Code{}, store.ErrGone
}

func (s *memory) Arrive(_ context.Context, guild, user snowflake.ID, at time.Time) (snowflake.ID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return 0, s.fail
	}
	s.arrived = append(s.arrived, fmt.Sprint(guild, "/", user))
	for i, c := range s.codes {
		if c.Guild == guild && c.SpentBy == user && c.Arrived.IsZero() {
			s.codes[i].Arrived = at
			return c.Owner, nil
		}
	}
	return 0, nil
}

func (s *memory) Leave(_ context.Context, guild, user snowflake.ID, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.left = append(s.left, fmt.Sprint(guild, "/", user))
	return s.fail
}

func (s *memory) Pending(_ context.Context, since time.Time) ([]store.Code, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.Code
	for _, c := range s.codes {
		if c.SpentBy != 0 && c.Arrived.IsZero() && c.SpentAt.After(since) {
			out = append(out, c)
		}
	}
	return out, s.fail
}

func (s *memory) Tree(_ context.Context, guild snowflake.ID) ([]store.Join, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tree[guild], s.fail
}

func (s *memory) SetEach(_ context.Context, guild snowflake.ID, n int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.each[guild] = n
	return nil
}

// unexpected is the REST calls fleet doesn't answer: nil, so one panics.
type unexpected = rest.Rest

// fleet is the Discord a test sees: servers, who is in which and since
// when, who is banned, and what cygnet asked of it. REST calls it doesn't
// expect hit the nil rest.Rest and panic.
type fleet struct {
	unexpected
	mu      sync.Mutex
	guilds  []discord.Guild
	members map[snowflake.ID][]snowflake.ID
	bans    map[snowflake.ID][]snowflake.ID
	fail    error // every member lookup fails with it
	addErr  error // the add answers with it
	added   []string
	jobs    []discord.TargetUsersJobStatusCode // the job's answers, in turn
	invites []discord.InviteCreate
	on      []snowflake.ID // the channel each invite opened on
	texts   []discord.GuildChannel
}

func (f *fleet) Guilds() []discord.Guild { return f.guilds }
func (f *fleet) Rest() rest.Rest         { return f }
func (f *fleet) Quiet() rest.Client      { return f }

var joinedAt = time.Date(2026, 10, 12, 11, 0, 0, 0, time.UTC)

func (f *fleet) GetMember(guild, user snowflake.ID, _ ...rest.RequestOpt) (*discord.Member, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	if slices.Contains(f.members[guild], user) {
		return &discord.Member{User: discord.User{ID: user, Username: fmt.Sprint("user", user)}, JoinedAt: &joinedAt}, nil
	}
	return nil, &rest.Error{Code: rest.JSONErrorCodeUnknownMember}
}

func (f *fleet) GetBan(guild, user snowflake.ID, _ ...rest.RequestOpt) (*discord.Ban, error) {
	if slices.Contains(f.bans[guild], user) {
		return &discord.Ban{}, nil
	}
	return nil, &rest.Error{Code: rest.JSONErrorCodeUnknownBan}
}

// Do is the quiet client: only the member add goes through it.
func (f *fleet) Do(e *rest.CompiledEndpoint, body, _ any, _ ...rest.RequestOpt) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e.Endpoint != rest.AddMember {
		return fmt.Errorf("unexpected %s", e.URL)
	}
	if f.addErr != nil {
		return f.addErr
	}
	f.added = append(f.added, body.(discord.MemberAdd).AccessToken)
	return nil
}

func (f *fleet) CreateInvite(ch snowflake.ID, c discord.InviteCreate, _ ...rest.RequestOpt) (*discord.Invite, error) {
	f.invites = append(f.invites, c)
	f.on = append(f.on, ch)
	return &discord.Invite{Code: "onlyyou"}, nil
}

func (f *fleet) GetInviteTargetUsersJobStatus(code string, _ ...rest.RequestOpt) (*discord.TargetUsersJobStatus, error) {
	if len(f.jobs) == 0 {
		return &discord.TargetUsersJobStatus{Status: discord.TargetUsersJobStatusCodeCompleted}, nil
	}
	st := f.jobs[0]
	f.jobs = f.jobs[1:]
	return &discord.TargetUsersJobStatus{Status: st}, nil
}

func (f *fleet) GetGuildChannels(snowflake.ID, ...rest.RequestOpt) ([]discord.GuildChannel, error) {
	return f.texts, nil
}

// Servers: 3 is coretest's, 30 another member 5 is in, 31 one they aren't.
// 50 is an admin of 3, and 60 owns 30.
func setup() (*Module, *memory) {
	s := &memory{each: map[snowflake.ID]int{}}
	f := &fleet{
		guilds:  []discord.Guild{{ID: 3, Name: "nest", OwnerID: 1}, {ID: 30, Name: "pond", OwnerID: 60}, {ID: 31, Name: "reeds", OwnerID: 1}},
		members: map[snowflake.ID][]snowflake.ID{3: {5, 50}, 30: {5, 60}, 31: {7}},
		bans:    map[snowflake.ID][]snowflake.ID{},
	}
	m := New(s, f, "https://cygnet.lol/", func(i discord.Interaction) bool { return i.User().ID == 50 }, guard.New(), slog.New(slog.DiscardHandler))
	m.now = func() time.Time { return time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC) }
	return m, s
}

// as makes an interaction payload come from user, or from a DM when dm is
// set.
func as(user string, dm bool) func(map[string]any) {
	return func(p map[string]any) {
		if dm {
			delete(p, "guild_id")
			delete(p, "member")
			p["user"] = map[string]any{"id": user, "username": "u"}
			p["channel"] = map[string]any{"id": "4", "type": 1}
			return
		}
		p["member"].(map[string]any)["user"] = map[string]any{"id": user, "username": "u"}
	}
}

func run(t *testing.T, m *Module, edit func(map[string]any)) (string, string, error) {
	t.Helper()
	e, sent := coretest.Event(t, "invite", edit)
	if err := m.open(context.Background(), e); err != nil {
		return "", "", err
	}
	if (*sent)[0].Flags&discord.MessageFlagEphemeral == 0 {
		t.Fatal("the panel is for the member alone")
	}
	return coretest.Text("", (*sent)[0].Components), ids((*sent)[0].Components), nil
}

// press is user choosing values (none for a button) on the component id.
func press(t *testing.T, m *Module, customID string, edit func(map[string]any), values ...string) (string, string, error) {
	t.Helper()
	e, _ := coretest.Button(t, customID, func(p map[string]any) {
		if edit != nil {
			edit(p)
		}
		if values != nil {
			v := make([]any, len(values))
			for i, s := range values {
				v[i] = s
			}
			p["data"] = map[string]any{"custom_id": customID, "component_type": 3, "values": v}
		}
	})
	var got discord.InteractionResponseData
	e.Respond = func(_ discord.InteractionResponseType, d discord.InteractionResponseData, _ ...rest.RequestOpt) error {
		got = d
		return nil
	}
	if err := m.click(context.Background(), e); err != nil {
		return "", "", err
	}
	u := got.(discord.MessageUpdate)
	return coretest.Text("", *u.Components), ids(*u.Components), nil
}

// ids is every interactive component's custom ID, a "!" before a
// disabled one, each followed by a space.
func ids(cs []discord.LayoutComponent) string {
	var b strings.Builder
	for _, c := range cs {
		box, ok := c.(discord.ContainerComponent)
		if !ok {
			continue
		}
		for _, s := range box.Components {
			row, ok := s.(discord.ActionRowComponent)
			if !ok {
				continue
			}
			for _, x := range row.Components {
				switch v := x.(type) {
				case discord.ButtonComponent:
					if v.Disabled {
						b.WriteString("!")
					}
					b.WriteString(v.CustomID + " ")
				case discord.StringSelectMenuComponent:
					b.WriteString(v.CustomID + " ")
				}
			}
		}
	}
	return b.String()
}

func has(t *testing.T, text string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(text, w) {
			t.Fatalf("no %q in\n%s", w, text)
		}
	}
}

func TestMemberMakesAndTakesBack(t *testing.T) {
	m, s := setup()
	text, got, err := run(t, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	has(t, text, "**invite** · nest", "haven't let members make links yet")
	has(t, got, "!invite:make:3 ")
	if strings.Contains(got, "invite:each:3") {
		t.Fatal("a member was offered the allowance")
	}
	if _, _, err := press(t, m, "invite:make:3", nil); err == nil || !strings.Contains(err.Error(), "no links left") {
		t.Fatalf("make with none allowed: %v", err)
	}

	s.each[3] = 2
	text, _, err = press(t, m, "invite:make:3", nil)
	if err != nil {
		t.Fatal(err)
	}
	code := regexp.MustCompile(`https://cygnet\.lol/i/([a-z2-7]{32})\n`).FindStringSubmatch(text)
	if code == nil {
		t.Fatalf("no link in\n%s", text)
	}
	h := sha256.Sum256([]byte(code[1]))
	if !bytes.Equal(s.codes[0].Hash, h[:]) || s.codes[0].Owner != 5 || s.codes[0].Guild != 3 || s.codes[0].Expires.Sub(s.codes[0].Made) != life {
		t.Fatalf("kept %+v", s.codes[0])
	}
	if strings.Contains(text, "invite:") {
		t.Fatal("custom IDs in the text")
	}
	has(t, text, "shown this once", "1 of 2 links left", "open · 2d 0h left")

	// Taken back, it's theirs to make again; the second one used stays used.
	label := hex.EncodeToString(h[:])
	text, _, err = press(t, m, "invite:back:3", nil, label)
	if err != nil {
		t.Fatal(err)
	}
	has(t, text, "✓ taken back", "2 of 2 links left", "`"+label[:4]+"` taken back")
	if _, _, err := press(t, m, "invite:back:3", nil, label); err == nil || !strings.Contains(err.Error(), "taken back already") {
		t.Fatalf("taking back twice: %v", err)
	}
	if _, _, err := press(t, m, "invite:back:3", nil, "zz"); err == nil {
		t.Fatal("a value that isn't a link")
	}

	for range 2 {
		if _, _, err := press(t, m, "invite:make:3", nil); err != nil {
			t.Fatal(err)
		}
	}
	s.codes[0].SpentBy, s.codes[0].SpentAt = 9, m.now().Add(-2*time.Hour)
	s.codes[1].Expires = m.now()
	text, got, _ = press(t, m, "invite:3", nil)
	has(t, text, "used by <@9> 2h 0m ago", "ran out", "1 of 2 links left")
	if strings.Contains(got, "invite:back:3") {
		t.Fatal("nothing open to take back, but a select for it")
	}
}

func TestAdmin(t *testing.T) {
	m, s := setup()
	text, got, err := run(t, m, as("50", false))
	if err != nil {
		t.Fatal(err)
	}
	has(t, text, "no limit")
	has(t, got, "invite:make:3 ", "invite:each:3 ")
	text, _, err = press(t, m, "invite:each:3", as("50", false), "3")
	if err != nil || s.each[3] != 3 {
		t.Fatalf("each %d: %v", s.each[3], err)
	}
	has(t, text, "each member may have 3 links")
	if text, _, _ := press(t, m, "invite:each:3", as("50", false), "0"); !strings.Contains(text, "members can't make links now") {
		t.Fatal(text)
	}
	for _, c := range []struct {
		edit  func(map[string]any)
		value string
	}{{nil, "3"}, {as("50", false), "4"}, {as("60", true), "3"}} {
		if _, _, err := press(t, m, "invite:each:3", c.edit, c.value); err == nil {
			t.Fatalf("each %s was set", c.value)
		}
	}
	// The owner is an admin from anywhere, for making links, but sets the
	// allowance only from inside.
	text, got, err = press(t, m, "invite:30", as("60", true))
	if err != nil {
		t.Fatal(err)
	}
	has(t, text, "**invite** · pond", "no limit")
	if strings.Contains(got, "invite:each:30") {
		t.Fatal("the allowance offered from a DM")
	}
}

// A server that turned /invite off has it off from everywhere.
func TestGated(t *testing.T) {
	m, s := setup()
	s.each[30] = 1
	m.Gate(func(g snowflake.ID) bool { return g != 30 })
	if _, _, err := run(t, m, as("5", true)); err == nil || !strings.Contains(err.Error(), "not in a server where") {
		t.Fatalf("an off server offered: %v", err)
	}
	if _, _, err := press(t, m, "invite:make:30", as("5", true)); err == nil || !strings.Contains(err.Error(), "off in that server") {
		t.Fatalf("a link made in an off server: %v", err)
	}
}

func TestFromElsewhere(t *testing.T) {
	m, s := setup()
	if _, _, err := run(t, m, as("5", true)); err == nil || !strings.Contains(err.Error(), "not in a server where") {
		t.Fatalf("none allowed: %v", err)
	}
	// One server lets members in, and they're in it: its panel at once.
	s.each[30] = 1
	s.each[31] = 1
	text, _, err := run(t, m, as("5", true))
	if err != nil {
		t.Fatal(err)
	}
	has(t, text, "**invite** · pond", "1 of 1 link left")
	// Two: a choice, in name order, and the pick opens the panel.
	s.each[3] = 2
	text, got, err := run(t, m, as("5", true))
	if err != nil {
		t.Fatal(err)
	}
	has(t, text, "which server")
	has(t, got, pickID+" ")
	text, _, err = press(t, m, pickID, as("5", true), "3")
	if err != nil {
		t.Fatal(err)
	}
	has(t, text, "**invite** · nest", "2 of 2 links left")
	if _, _, err := press(t, m, "invite:make:31", as("5", true)); err == nil || !strings.Contains(err.Error(), "not in that server") {
		t.Fatalf("someone else's server: %v", err)
	}
	if _, _, err := press(t, m, "invite:make:99", as("5", true)); err == nil || !strings.Contains(err.Error(), "isn't in that server") {
		t.Fatalf("a server cygnet left: %v", err)
	}
	if _, _, err := press(t, m, "invite:make:x", nil); err == nil || !strings.Contains(err.Error(), "out of date") {
		t.Fatalf("a mangled id: %v", err)
	}
	if _, _, err := press(t, m, pickID, nil); err == nil {
		t.Fatal("a pick with nothing picked")
	}
	// From outside, made links work as in the server.
	text, _, err = press(t, m, "invite:make:3", as("5", true))
	if err != nil {
		t.Fatal(err)
	}
	has(t, text, "shown this once", "1 of 2 links left")
}

func TestRefusals(t *testing.T) {
	m, _ := setup()
	m.edge = ""
	if _, _, err := run(t, m, nil); !errors.Is(err, errOffline) {
		t.Fatalf("offline: %v", err)
	}
	if _, _, err := press(t, m, "invite:3", nil); !errors.Is(err, errOffline) {
		t.Fatalf("offline click: %v", err)
	}
	m, s := setup()
	s.fail = errors.New("db down")
	if _, _, err := run(t, m, nil); err == nil {
		t.Fatal("a store failure")
	}
	m, s = setup()
	s.each[30] = 1
	m.discord.(*fleet).fail = errors.New("discord down")
	if _, _, err := run(t, m, as("5", true)); err == nil || !strings.Contains(err.Error(), "discord down") {
		t.Fatalf("discord failing: %v", err)
	}
	if _, _, err := press(t, m, "invite:30", as("5", true)); err == nil {
		t.Fatal("discord failing on a click")
	}
}

func TestModule(t *testing.T) {
	m, _ := setup()
	if m.Name() != "invite" || m.Perms() == 0 || m.Want().Required == 0 || m.Help().About == "" || len(m.Components()) != 1 {
		t.Fatal("module shape")
	}
	c := m.Commands()[0]
	if c.Create.CommandName() != "invite" {
		t.Fatal(c.Create.CommandName())
	}
	code, h := newCode()
	if len(code) != 32 || len(h) != 32 || code != strings.ToLower(code) {
		t.Fatalf("code %q", code)
	}
	if a, _ := newCode(); a == code {
		t.Fatal("two codes alike")
	}
	if links(1) != "1 link" || links(3) != "3 links" {
		t.Fatal("links")
	}
}

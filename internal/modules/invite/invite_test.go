package invite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/core/coretest"
	"github.com/6586x57890143/cygnet/internal/store"
)

// memory is the store as a test keeps it, with Codes' rules.
type memory struct {
	mu    sync.Mutex
	codes []store.Code
	each  map[snowflake.ID]int
	fail  error
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

func (s *memory) SetEach(_ context.Context, guild snowflake.ID, n int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.each[guild] = n
	return nil
}

// fleet is the Discord a test sees: servers, and who is in which.
type fleet struct {
	guilds  []discord.Guild
	members map[snowflake.ID][]snowflake.ID
	fail    error
}

func (f fleet) Guilds() []discord.Guild { return f.guilds }

func (f fleet) Member(_ context.Context, guild, user snowflake.ID) (bool, error) {
	for _, u := range f.members[guild] {
		if u == user {
			return true, f.fail
		}
	}
	return false, f.fail
}

// Servers: 3 is coretest's, 30 another member 5 is in, 31 one they aren't.
// 50 is an admin of 3, and 60 owns 30.
func setup() (*Module, *memory) {
	s := &memory{each: map[snowflake.ID]int{}}
	f := fleet{
		guilds:  []discord.Guild{{ID: 3, Name: "nest", OwnerID: 1}, {ID: 30, Name: "pond", OwnerID: 60}, {ID: 31, Name: "reeds", OwnerID: 1}},
		members: map[snowflake.ID][]snowflake.ID{3: {5, 50}, 30: {5, 60}, 31: {7}},
	}
	m := New(s, f, "https://cygnet.lol/", func(i discord.Interaction) bool { return i.User().ID == 50 })
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
	f := m.discord.(fleet)
	f.fail = errors.New("discord down")
	m.discord = f
	if _, _, err := run(t, m, as("5", true)); err == nil || !strings.Contains(err.Error(), "discord down") {
		t.Fatalf("discord failing: %v", err)
	}
	if _, _, err := press(t, m, "invite:30", as("5", true)); err == nil {
		t.Fatal("discord failing on a click")
	}
}

func TestModule(t *testing.T) {
	m, _ := setup()
	if m.Name() != "invite" || m.Perms() != 0 || m.Want().Required == 0 || m.Help().About == "" || len(m.Components()) != 1 {
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

package invite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/guard"
	"github.com/6586x57890143/cygnet/internal/store"
	"github.com/6586x57890143/cygnet/link"
)

// A redemption's people: 5 owns the code in server 3, newcomer's account is
// two months old, young's a day.
var (
	now      = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	newcomer = snowflake.New(now.Add(-60 * 24 * time.Hour))
	young    = snowflake.New(now.Add(-24 * time.Hour))
)

// minted is setup with a code of 5's in server 3, whose system channel is
// 40, and the code's hash in hex.
func minted(t *testing.T) (*Module, *memory, *fleet, string) {
	t.Helper()
	m, s := setup()
	f := m.discord.(*fleet)
	door := snowflake.ID(40)
	f.guilds[0].SystemChannelID = &door
	h := sha256.Sum256([]byte("code"))
	if err := s.Mint(context.Background(), h[:], 3, 5, now.Add(-time.Hour), now.Add(47*time.Hour), -1); err != nil {
		t.Fatal(err)
	}
	return m, s, f, hex.EncodeToString(h[:])
}

func call(t *testing.T, fn func(context.Context, string, json.RawMessage) (any, error), caller string, body any) (any, error) {
	t.Helper()
	raw, _ := json.Marshal(body)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return fn(ctx, caller, raw)
}

// denied is whether err refuses with reason.
func denied(err error, reason string) bool {
	le, ok := errors.AsType[*link.Error](err)
	return ok && le.Code == link.Denied && le.Reason == reason
}

func TestCode(t *testing.T) {
	m, s, f, code := minted(t)
	got, err := call(t, m.Code, Edge, CodeAsk{code})
	info := got.(CodeInfo)
	if err != nil || !info.Open || info.Guild != "3" || info.Name != "nest" || info.From != "user5" || info.Expires != now.Add(47*time.Hour).UnixMilli() {
		t.Fatalf("an open code: %+v %v", got, err)
	}
	if _, err := call(t, m.Code, "skua", CodeAsk{code}); !errors.Is(err, link.Err(link.Denied)) {
		t.Fatalf("a bird that isn't the Worker: %v", err)
	}
	for _, body := range []any{"not an object", CodeAsk{"zz"}, CodeAsk{code[:10]}} {
		if _, err := call(t, m.Code, Edge, body); !errors.Is(err, link.Err(link.Invalid)) {
			t.Fatalf("%v: %v", body, err)
		}
	}
	closed := func(why string) {
		t.Helper()
		got, err := call(t, m.Code, Edge, CodeAsk{code})
		if err != nil || got.(CodeInfo).Open {
			t.Fatalf("%s: %+v %v", why, got, err)
		}
	}
	other := sha256.Sum256([]byte("other"))
	if got, _ := call(t, m.Code, Edge, CodeAsk{hex.EncodeToString(other[:])}); got.(CodeInfo).Open {
		t.Fatal("no such code, open")
	}

	// Spent by someone who hasn't arrived: open for them, for a day.
	s.codes[0].SpentBy, s.codes[0].SpentAt = newcomer, now.Add(-time.Hour)
	if got, _ := call(t, m.Code, Edge, CodeAsk{code}); !got.(CodeInfo).Open {
		t.Fatal("spent, waiting for its spender, closed")
	}
	s.codes[0].SpentAt = now.Add(-25 * time.Hour)
	closed("spent a day ago")
	s.codes[0].SpentBy, s.codes[0].SpentAt = 0, time.Time{}

	m.Gate(func(g snowflake.ID) bool { return g != 3 })
	closed("its server has /invite off")
	m.Gate(func(snowflake.ID) bool { return true })
	f.members[3] = []snowflake.ID{50}
	closed("its owner left")
	f.members[3] = []snowflake.ID{5, 50}
	s.codes[0].Revoked = now
	closed("taken back")
	s.codes[0].Revoked = time.Time{}
	f.fail = errors.New("discord down")
	if _, err := call(t, m.Code, Edge, CodeAsk{code}); err == nil {
		t.Fatal("discord failing, no error")
	}
}

// A launch's invite puts someone in line: its page is the launch's, and it
// never lets anyone straight into the server.
func TestLaunchCode(t *testing.T) {
	m, s, _, code := minted(t)
	s.codes[0].Launch = "silcord"
	got, err := call(t, m.Code, Edge, CodeAsk{code})
	if info := got.(CodeInfo); err != nil || !info.Open || info.Launch != "silcord" || info.Guild != "" || info.Expires != now.Add(47*time.Hour).UnixMilli() {
		t.Fatalf("a launch's invite: %+v %v", got, err)
	}
	if _, err := call(t, m.Redeem, Edge, RedeemAsk{Code: code, User: newcomer.String()}); !denied(err, Gone) {
		t.Fatalf("redeeming a launch's invite: %v", err)
	}
	s.codes[0].Revoked = now
	if got, _ := call(t, m.Code, Edge, CodeAsk{code}); got.(CodeInfo).Open {
		t.Fatal("a launch's invite taken back, open")
	}
}

func TestRedeemAdds(t *testing.T) {
	m, s, f, code := minted(t)
	got, err := call(t, m.Redeem, Edge, RedeemAsk{code, newcomer.String(), "tok"})
	if err != nil || got != (Redeemed{Guild: "3", Added: true}) {
		t.Fatalf("redeem: %+v %v", got, err)
	}
	if len(f.added) != 1 || f.added[0] != "tok" || s.codes[0].SpentBy != newcomer || s.codes[0].Arrived.IsZero() {
		t.Fatalf("added %v, code %+v", f.added, s.codes[0])
	}
	// The Worker retries: they're in, so nothing more is spent or added.
	f.members[3] = append(f.members[3], newcomer)
	spends := s.spends
	got, err = call(t, m.Redeem, Edge, RedeemAsk{code, newcomer.String(), "tok"})
	if err != nil || got != (Redeemed{Guild: "3", Added: true}) || s.spends != spends || len(f.added) != 1 {
		t.Fatalf("a retry: %+v %v, spends %d, added %v", got, err, s.spends, f.added)
	}
	// Someone already in another way spends nothing either.
	if got, err := call(t, m.Redeem, Edge, RedeemAsk{code, "50", ""}); err != nil || !got.(Redeemed).Added || s.spends != spends {
		t.Fatalf("a member: %+v %v", got, err)
	}
}

func TestRedeemInvites(t *testing.T) {
	m, s, f, code := minted(t)
	f.jobs = []discord.TargetUsersJobStatusCode{discord.TargetUsersJobStatusCodeProcessing, discord.TargetUsersJobStatusCodeProcessing}
	got, err := call(t, m.Redeem, Edge, RedeemAsk{Code: code, User: newcomer.String()})
	if err != nil || got != (Redeemed{Guild: "3", Invite: "onlyyou"}) {
		t.Fatalf("redeem: %+v %v", got, err)
	}
	inv := f.invites[0]
	if f.on[0] != 40 || *inv.MaxAge != inviteAge || *inv.MaxUses != 1 || !inv.Unique || !slices.Equal(inv.TargetUserIDs, []snowflake.ID{newcomer}) || len(f.jobs) != 0 {
		t.Fatalf("invite %+v on %v, jobs left %v", inv, f.on, f.jobs)
	}
	if s.codes[0].SpentBy != newcomer || !s.codes[0].Arrived.IsZero() {
		t.Fatalf("spent and not yet arrived: %+v", s.codes[0])
	}
	// They never used it and come back: theirs again, a fresh invite.
	if got, err := call(t, m.Redeem, Edge, RedeemAsk{Code: code, User: newcomer.String()}); err != nil || got.(Redeemed).Invite == "" || len(f.invites) != 2 {
		t.Fatalf("coming back: %+v %v", got, err)
	}
	// A token Discord won't take leaves the invite.
	f.addErr = &rest.Error{Code: rest.JSONErrorCodeInvalidOAuth2AccessToken, Message: "bad token"}
	if got, err := call(t, m.Redeem, Edge, RedeemAsk{code, newcomer.String(), "stale"}); err != nil || got.(Redeemed).Invite == "" {
		t.Fatalf("a refused token: %+v %v", got, err)
	}
	f.jobs = []discord.TargetUsersJobStatusCode{discord.TargetUsersJobStatusCodeFailed}
	if _, err := call(t, m.Redeem, Edge, RedeemAsk{Code: code, User: newcomer.String()}); err == nil {
		t.Fatal("a failed job, an invite")
	}
}

func TestRedeemRefuses(t *testing.T) {
	m, s, f, code := minted(t)
	cases := []struct {
		why    string
		caller string
		ask    RedeemAsk
		reason string
	}{
		{"a young account", Edge, RedeemAsk{code, young.String(), ""}, Young},
		{"no such code", Edge, RedeemAsk{hex.EncodeToString(make([]byte, 32)), newcomer.String(), ""}, Gone},
	}
	for _, c := range cases {
		_, err := call(t, m.Redeem, c.caller, c.ask)
		if !denied(err, c.reason) {
			t.Fatalf("%s: %v", c.why, err)
		}
	}
	f.bans[3] = []snowflake.ID{newcomer}
	if _, err := call(t, m.Redeem, Edge, RedeemAsk{code, newcomer.String(), ""}); !denied(err, Banned) {
		t.Fatalf("banned: %v", err)
	}
	f.bans[3] = nil
	if _, err := call(t, m.Redeem, "skua", RedeemAsk{code, newcomer.String(), ""}); !errors.Is(err, link.Err(link.Denied)) {
		t.Fatalf("not the Worker: %v", err)
	}
	if _, err := call(t, m.Redeem, Edge, RedeemAsk{code, "me", ""}); !errors.Is(err, link.Err(link.Invalid)) {
		t.Fatalf("a user that isn't an ID: %v", err)
	}
	if _, err := call(t, m.Redeem, Edge, RedeemAsk{"zz", newcomer.String(), ""}); !errors.Is(err, link.Err(link.Invalid)) {
		t.Fatalf("a code that isn't a hash: %v", err)
	}
	if s.spends != 0 {
		t.Fatalf("%d spent on refusals", s.spends)
	}

	// Discord's own refusals, after the spend.
	for code2, reason := range map[rest.JSONErrorCode]string{rest.JSONErrorCodeUserBannedFromGuild: Banned, rest.JSONErrorCodeMaximumGuildsReached: Full} {
		f.addErr = &rest.Error{Code: code2}
		if _, err := call(t, m.Redeem, Edge, RedeemAsk{code, newcomer.String(), "tok"}); !denied(err, reason) {
			t.Fatalf("%d: %v", code2, err)
		}
	}
	// cygnet lacking a permission is the server not being ready, said as
	// such; she reports it to its admins herself.
	f.addErr = &rest.Error{Code: rest.JSONErrorCodeLackPermissionsToPerformAction}
	if _, err := call(t, m.Redeem, Edge, RedeemAsk{code, newcomer.String(), "tok"}); !denied(err, Unready) {
		t.Fatalf("missing permissions: %v", err)
	}
	// Discord failing keeps the code theirs for a retry, and says nothing
	// of the request.
	f.addErr = &rest.Error{Response: &http.Response{Status: "503 Service Unavailable", StatusCode: 503}, RqBody: []byte(`{"access_token":"tok"}`)}
	_, err := call(t, m.Redeem, Edge, RedeemAsk{code, newcomer.String(), "tok"})
	if re, ok := errors.AsType[*rest.Error](err); !ok || re.RqBody != nil || re.Response.StatusCode != 503 {
		t.Fatalf("a 503: %#v", err)
	}
	if len(f.invites) != 0 || s.codes[0].SpentBy != newcomer {
		t.Fatal("a 503 fell back to an invite, or unspent the code")
	}

	// Spent by someone else meanwhile, or taken back: gone.
	other := snowflake.New(now.Add(-90 * 24 * time.Hour))
	if _, err := call(t, m.Redeem, Edge, RedeemAsk{code, other.String(), ""}); !denied(err, Gone) {
		t.Fatalf("spent by another: %v", err)
	}
	f.fail = errors.New("discord down")
	if _, err := call(t, m.Redeem, Edge, RedeemAsk{code, other.String(), ""}); err == nil || denied(err, Gone) {
		t.Fatalf("discord failing: %v", err)
	}
}

// The guard's cap holds joins back past it.
func TestRedeemBusy(t *testing.T) {
	m2, _, _, code2 := minted(t)
	m2.guard = guard.New()
	for m2.guard.Allow(3, guard.MemberAdd) == nil {
	}
	for m2.guard.Allow(3, guard.InviteCreate) == nil {
	}
	if _, err := call(t, m2.Redeem, Edge, RedeemAsk{code2, newcomer.String(), "tok"}); !errors.Is(err, link.Err(link.Busy)) {
		t.Fatalf("over the cap, adding: %v", err)
	}
	m3, _, _, code3 := minted(t)
	m3.guard = m2.guard
	if _, err := call(t, m3.Redeem, Edge, RedeemAsk{Code: code3, User: newcomer.String()}); !errors.Is(err, link.Err(link.Busy)) {
		t.Fatalf("over the cap, inviting: %v", err)
	}
}

// Its owner is in the server, so their own link lets them through as in,
// spending nothing; one who left took it with them.
func TestRedeemOwn(t *testing.T) {
	m, s, f, code := minted(t)
	if got, err := call(t, m.Redeem, Edge, RedeemAsk{code, "5", ""}); err != nil || !got.(Redeemed).Added || s.spends != 0 {
		t.Fatalf("the owner: %+v %v", got, err)
	}
	f.members[3] = []snowflake.ID{50}
	if _, err := call(t, m.Redeem, Edge, RedeemAsk{code, newcomer.String(), ""}); !denied(err, Gone) {
		t.Fatalf("its owner left: %v", err)
	}
}

// storeCode is a code of owner's in guild, spent by by at at.
func storeCode(guild, owner, by snowflake.ID, at time.Time) store.Code {
	h := sha256.Sum256([]byte(by.String()))
	return store.Code{Hash: h[:], Guild: guild, Owner: owner, SpentBy: by, SpentAt: at, Made: at, Expires: at.Add(life)}
}

func TestDoor(t *testing.T) {
	m, _ := setup()
	f := m.discord.(*fleet)
	text := func(raw string) discord.GuildChannel {
		var u discord.UnmarshalChannel
		if err := json.Unmarshal([]byte(raw), &u); err != nil {
			t.Fatal(err)
		}
		return u.Channel.(discord.GuildChannel)
	}
	ctx := context.Background()
	if _, err := m.door(ctx, f.guilds[0]); err == nil {
		t.Fatal("a door in a server with no text channel")
	}
	f.texts = []discord.GuildChannel{
		text(`{"id":"41","type":0,"guild_id":"3","position":2}`),
		text(`{"id":"42","type":2,"guild_id":"3","position":0}`),
		text(`{"id":"43","type":0,"guild_id":"3","position":1}`),
	}
	if ch, err := m.door(ctx, f.guilds[0]); err != nil || ch != 43 {
		t.Fatalf("door %v %v, want the first text channel, 43", ch, err)
	}
	rules := snowflake.ID(44)
	f.guilds[0].RulesChannelID = &rules
	if ch, _ := m.door(ctx, f.guilds[0]); ch != 44 {
		t.Fatalf("door %v, want the rules, 44", ch)
	}
}

// eventually waits for what an event set off to show in got.
func eventually(t *testing.T, s *memory, got func() []string, want string) {
	t.Helper()
	for range 200 {
		s.mu.Lock()
		ok := slices.Contains(got(), want)
		s.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no %s in %v", want, got())
}

func TestJoins(t *testing.T) {
	m, s, f, _ := minted(t)
	m.OnEvent(&events.GuildMemberJoin{GenericGuildMember: &events.GenericGuildMember{GuildID: 3, Member: discord.Member{User: discord.User{ID: 77, Bot: true}}}})
	m.OnEvent(&events.GuildMemberJoin{GenericGuildMember: &events.GenericGuildMember{GuildID: 3, Member: discord.Member{User: discord.User{ID: 78}}}})
	eventually(t, s, func() []string { return s.arrived }, "3/78")
	if slices.Contains(s.arrived, "3/77") {
		t.Fatal("a bot's join recorded")
	}
	m.OnEvent(&events.GuildMemberLeave{GuildID: 3, User: discord.User{ID: 78}})
	eventually(t, s, func() []string { return s.left }, "3/78")

	// The sweep: one spent code's spender is in now, another's isn't, and
	// one is in a server with /invite off.
	s.codes[0].SpentBy, s.codes[0].SpentAt = newcomer, now.Add(-time.Hour)
	f.members[3] = append(f.members[3], newcomer)
	s.codes = append(s.codes,
		storeCode(3, 5, 91, now.Add(-time.Hour)),
		storeCode(30, 60, 92, now.Add(-time.Hour)),
	)
	f.members[30] = append(f.members[30], 92)
	m.Gate(func(g snowflake.ID) bool { return g != 30 })
	m.OnEvent(&events.GuildsReady{})
	eventually(t, s, func() []string { return s.arrived }, "3/"+newcomer.String())
	if slices.Contains(s.arrived, "3/91") || slices.Contains(s.arrived, "30/92") {
		t.Fatalf("swept %v", s.arrived)
	}

	// Failures are logged, not fatal.
	s.mu.Lock()
	s.fail = errors.New("db down")
	s.mu.Unlock()
	m.sweep(context.Background())
	s.mu.Lock()
	s.fail = nil
	s.mu.Unlock()
	f.mu.Lock()
	f.fail = errors.New("discord down")
	f.mu.Unlock()
	m.sweep(context.Background())
	if (m.joined(discord.Member{})) != m.now() {
		t.Fatal("a member with no join time")
	}
}

func TestTree(t *testing.T) {
	m, s := setup()
	s.tree = map[snowflake.ID][]store.Join{3: {
		{User: 11, Sponsor: 5, At: now},
		{User: 12, At: now},
		{User: 13, Sponsor: 5, At: now, Left: now},
		{User: 14, Sponsor: 11, At: now},
	}}
	text, _, err := press(t, m, "invite:tree:3", as("50", false))
	if err != nil {
		t.Fatal(err)
	}
	has(t, text, "**invite** · the tree", "joined      4 since cygnet watched", "by a link   3", "untraced    1", "left since  1",
		"<@5> brought <@11> · ~~<@13>~~\n<@11> brought <@14>\n-# untraced: <@12>\n")
	if _, got, _ := press(t, m, "invite:3", as("50", false)); !strings.Contains(got, "invite:tree:3 ") {
		t.Fatal("no tree button for an admin")
	}
	if _, got, _ := press(t, m, "invite:3", nil); strings.Contains(got, "invite:tree:3") {
		t.Fatal("a tree button for a member")
	}
	for _, edit := range []func(map[string]any){nil, as("5", true)} {
		if _, _, err := press(t, m, "invite:tree:3", edit); err == nil || !strings.Contains(err.Error(), "only this server's admins") {
			t.Fatalf("the tree for someone else: %v", err)
		}
	}

	// Nobody yet, and a server too big for one card.
	s.tree[3] = nil
	if text, _, _ := press(t, m, "invite:tree:3", as("50", false)); !strings.Contains(text, "nobody has joined since cygnet started watching") {
		t.Fatal(text)
	}
	// 300 sponsors with one each, and one who brought 130: past one card,
	// in pages, each inside Discord's 4000, the 130 over three lines.
	for i := range 300 {
		s.tree[3] = append(s.tree[3], store.Join{User: snowflake.ID(1_000_000_000_000_000 + i), Sponsor: snowflake.ID(2_000_000_000_000_000 + i), At: now})
	}
	for i := range 130 {
		s.tree[3] = append(s.tree[3], store.Join{User: snowflake.ID(3_000_000_000_000_000 + i), Sponsor: 5, At: now})
	}
	seen := map[string]bool{}
	for page := 0; ; page++ {
		text, got, err := press(t, m, fmt.Sprintf("invite:tree:%d:3", page), as("50", false))
		if err != nil {
			t.Fatal(err)
		}
		if len(text) > 4000 {
			t.Fatalf("page %d: %d characters", page, len(text))
		}
		for _, l := range strings.Split(text, "\n") {
			if strings.HasPrefix(l, "<@") {
				if seen[l] {
					t.Fatalf("on two pages: %s", l[:40])
				}
				seen[l] = true
			}
		}
		if page == 0 && !strings.Contains(got, "!invite:tree:-1:3 invite:tree:1:3 ") {
			t.Fatalf("first page's buttons: %s", got)
		}
		if strings.Contains(text, fmt.Sprintf("page %d of %d", page+1, page+1)) {
			if !strings.Contains(got, fmt.Sprintf("!invite:tree:%d:3", page+1)) {
				t.Fatalf("the last page's later button works: %s", got)
			}
			break
		}
		if page > 20 {
			t.Fatal("no last page")
		}
	}
	if len(seen) != 300+3 {
		t.Fatalf("%d lines over the pages, want 303", len(seen))
	}
	// A page past the end shows the last; one before the start, the first.
	far, _, _ := press(t, m, "invite:tree:99:3", as("50", false))
	if !strings.Contains(far, "page ") || strings.Contains(far, "page 1 of") {
		t.Fatal("page 99 wasn't the last")
	}
	if first, _, _ := press(t, m, "invite:tree:-5:3", as("50", false)); !strings.Contains(first, "page 1 of") {
		t.Fatal("page -5 wasn't the first")
	}
	brought, also := 0, 0
	for l := range seen {
		switch {
		case strings.HasPrefix(l, "<@5> brought "):
			brought++
		case strings.HasPrefix(l, "<@5> also brought "):
			also++
		}
	}
	if brought != 1 || also != 2 {
		t.Fatalf("130 brought by 5: %d lines, %d more", brought, also)
	}
	s.fail = errors.New("db down")
	if _, _, err := press(t, m, "invite:tree:3", as("50", false)); err == nil {
		t.Fatal("a store failure")
	}
}

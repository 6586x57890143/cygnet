package invite

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/guard"
	"github.com/6586x57890143/cygnet/internal/store"
	"github.com/6586x57890143/cygnet/link"
)

// Redemption (SPEC.md, Membership): cygnet.lol's Worker links to the hub
// as the bird Edge and asks two things over POST /v1/call. cygnet.code is
// what a link's page shows before anyone signs in; cygnet.redeem spends the
// code for whoever signed in and lets them in, by adding them with their
// own sign in or, without one, with a Discord invite only they can take.
// The code is spent before anyone is let in, and spending it again for the
// same person returns it, so the Worker can retry.

// Edge is the name cygnet.lol's Worker links to the hub as: the only
// caller either verb answers.
const Edge = "edge"

const (
	// minAge is how old a Discord account has to be to come in by a code,
	// as on cygnet.lol's list.
	minAge = 30 * 24 * time.Hour
	// inviteAge is how long, in seconds, the invite made for someone who'd
	// rather not be added stays good.
	inviteAge = 300
	// jobEvery is how often cygnet asks whether Discord has finished
	// limiting an invite to its one person.
	jobEvery = 200 * time.Millisecond
)

// Why a redemption is refused, as a denied error's reason, for the Worker
// to say in its own words.
const (
	// Gone: no such code, or it was spent, taken back or ran out, or whoever
	// made it left the server. Its owner, who is in the server, is let
	// through as already in.
	Gone = "gone"
	// Banned: they are banned from the server.
	Banned = "banned"
	// Young: their Discord account is newer than minAge.
	Young = "young"
	// Full: they are in as many servers as Discord allows.
	Full = "full"
	// Unready: Discord refused cygnet for want of a permission. She reports
	// it to the server's admins herself (perms.Watch in main); nothing was
	// spent that could have been.
	Unready = "unready"
)

// CodeAsk is cygnet.code's body: the SHA-256 of a link's code, in hex.
type CodeAsk struct {
	Code string `json:"code"`
}

// CodeInfo is what a link's page shows: whether it still works and, if
// it does, the server it lets someone into, who it's from, and when it
// stops working (Unix milliseconds). A link spent by someone who hasn't
// arrived yet stays open for a day, so they can come back to it: only
// they can get any further with it. A launch's invite says only its
// launch and when it stops working: it puts someone in line, and the
// launch's page says the rest.
type CodeInfo struct {
	Open    bool   `json:"open"`
	Launch  string `json:"launch,omitempty"`
	Guild   string `json:"guild,omitempty"`
	Name    string `json:"name,omitempty"`
	Icon    string `json:"icon,omitempty"`
	From    string `json:"from,omitempty"`
	Expires int64  `json:"expires,omitempty"`
}

// RedeemAsk is cygnet.redeem's body: the code's hash, who signed in, and
// their access token if they let cygnet add them (guilds.join). The token
// is used for that one request and kept nowhere.
type RedeemAsk struct {
	Code  string `json:"code"`
	User  string `json:"user"`
	Token string `json:"token,omitempty"`
}

// Redeemed is how they get in: they are in the server now (Added), or the
// Discord invite made for them alone is Invite.
type Redeemed struct {
	Guild  string `json:"guild"`
	Added  bool   `json:"added,omitempty"`
	Invite string `json:"invite,omitempty"`
}

// Code answers cygnet.code.
func (m *Module) Code(ctx context.Context, caller string, body json.RawMessage) (any, error) {
	var ask CodeAsk
	hash, err := read(caller, body, &ask, &ask.Code)
	if err != nil {
		return nil, err
	}
	now := m.now()
	if c, err := m.store.Find(ctx, hash); err == nil && c.Launch != "" {
		if !c.Open(now) {
			return CodeInfo{}, nil
		}
		return CodeInfo{Open: true, Launch: c.Launch, Expires: c.Expires.UnixMilli()}, nil
	}
	c, g, owner, err := m.lookup(ctx, hash)
	waiting := c.SpentBy != 0 && c.Arrived.IsZero() && now.Sub(c.SpentAt) < 24*time.Hour
	if errors.Is(err, errGone) || err == nil && !c.Open(now) && !waiting {
		return CodeInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	info := CodeInfo{Open: true, Guild: g.ID.String(), Name: g.Name, From: owner.EffectiveName(), Expires: c.Expires.UnixMilli()}
	if u := g.IconURL(); u != nil {
		info.Icon = *u
	}
	return info, nil
}

// Redeem answers cygnet.redeem: the gate (not banned, an account old
// enough), then the code spent, then in.
func (m *Module) Redeem(ctx context.Context, caller string, body json.RawMessage) (out any, err error) {
	defer func() {
		if rest.IsJSONErrorCode(err, rest.JSONErrorCodeLackPermissionsToPerformAction) {
			out, err = nil, link.Err(link.Denied, Unready)
		}
	}()
	return m.redeem(ctx, caller, body)
}

func (m *Module) redeem(ctx context.Context, caller string, body json.RawMessage) (any, error) {
	var ask RedeemAsk
	hash, err := read(caller, body, &ask, &ask.Code)
	if err != nil {
		return nil, err
	}
	user, err := snowflake.Parse(ask.User)
	if err != nil {
		return nil, link.Err(link.Invalid, "user is a Discord ID")
	}
	c, g, _, err := m.lookup(ctx, hash)
	if errors.Is(err, errGone) {
		return nil, link.Err(link.Denied, Gone)
	}
	if err != nil {
		return nil, err
	}
	now := m.now()
	// Already in: by this code a moment ago (a retry), or some other way.
	// Either way nothing is spent.
	mem, err := m.member(ctx, g.ID, user)
	if err != nil {
		return nil, err
	}
	if mem != nil {
		if c.SpentBy == user {
			m.arrive(ctx, g.ID, user, m.joined(*mem))
		}
		return Redeemed{Guild: g.ID.String(), Added: true}, nil
	}
	if now.Sub(user.Time()) < minAge {
		return nil, link.Err(link.Denied, Young)
	}
	if _, err := m.discord.Rest().GetBan(g.ID, user, rest.WithCtx(ctx)); err == nil {
		return nil, link.Err(link.Denied, Banned)
	} else if !rest.IsJSONErrorCode(err, rest.JSONErrorCodeUnknownBan) {
		return nil, err
	}
	switch _, err := m.store.Spend(ctx, hash, user, now); {
	case errors.Is(err, store.ErrGone), errors.Is(err, store.ErrOwn):
		return nil, link.Err(link.Denied, Gone)
	case err != nil:
		return nil, err
	}
	return m.in(ctx, g, user, ask.Token)
}

// Let lets user into guild by a cause cygnet already holds, a launch's
// wave: added with token, their own sign in, or else with a Discord invite
// only they can take. Someone already in is let through as added; someone
// banned is refused Banned. launch reaches it through main, as its Door.
func (m *Module) Let(ctx context.Context, guild, user snowflake.ID, token string) (out Redeemed, err error) {
	defer func() {
		if rest.IsJSONErrorCode(err, rest.JSONErrorCodeLackPermissionsToPerformAction) {
			out, err = Redeemed{}, link.Err(link.Denied, Unready)
		}
	}()
	g, ok := m.guild(guild)
	if !ok {
		return Redeemed{}, link.Err(link.Denied, Gone)
	}
	mem, err := m.member(ctx, guild, user)
	if err != nil {
		return Redeemed{}, err
	}
	if mem != nil {
		m.arrive(ctx, guild, user, m.joined(*mem))
		return Redeemed{Guild: guild.String(), Added: true}, nil
	}
	if _, err := m.discord.Rest().GetBan(guild, user, rest.WithCtx(ctx)); err == nil {
		return Redeemed{}, link.Err(link.Denied, Banned)
	} else if !rest.IsJSONErrorCode(err, rest.JSONErrorCodeUnknownBan) {
		return Redeemed{}, err
	}
	return m.in(ctx, g, user, token)
}

// in lets user into g once their cause is kept: added with token if they
// gave one Discord takes, or else with an invite only they can take.
func (m *Module) in(ctx context.Context, g discord.Guild, user snowflake.ID, token string) (Redeemed, error) {
	if token != "" {
		err := m.add(ctx, g.ID, user, token)
		if err == nil {
			m.arrive(ctx, g.ID, user, m.now())
			return Redeemed{Guild: g.ID.String(), Added: true}, nil
		}
		switch {
		case rest.IsJSONErrorCode(err, rest.JSONErrorCodeUserBannedFromGuild):
			return Redeemed{}, link.Err(link.Denied, Banned)
		case rest.IsJSONErrorCode(err, rest.JSONErrorCodeMaximumGuildsReached):
			return Redeemed{}, link.Err(link.Denied, Full)
		// A token Discord won't take (expired, revoked, without
		// guilds.join) leaves the other way in.
		case !rest.IsJSONErrorCode(err, rest.JSONErrorCodeInvalidOAuth2AccessToken, rest.JSONErrorCodeMissingRequiredOAuth2Scope):
			return Redeemed{}, err
		}
	}
	code, err := m.invite(ctx, g, user)
	if err != nil {
		return Redeemed{}, err
	}
	return Redeemed{Guild: g.ID.String(), Invite: code}, nil
}

var errGone = errors.New("invite: the code can't be used")

// lookup is the code hash names, with its server and its owner as a member
// of it: errGone if there's no such code, it's a launch's (which puts
// someone in line, not in the server), cygnet isn't in its server or has
// /invite off there, or its owner left, which takes their links with them.
func (m *Module) lookup(ctx context.Context, hash []byte) (store.Code, discord.Guild, *discord.Member, error) {
	c, err := m.store.Find(ctx, hash)
	if errors.Is(err, store.ErrGone) || err == nil && c.Launch != "" {
		return c, discord.Guild{}, nil, errGone
	}
	if err != nil {
		return c, discord.Guild{}, nil, err
	}
	g, ok := m.guild(c.Guild)
	if !ok || !m.on(g.ID) {
		return c, g, nil, errGone
	}
	owner, err := m.member(ctx, g.ID, c.Owner)
	if err == nil && owner == nil {
		err = errGone
	}
	return c, g, owner, err
}

// add puts user in guild with their own sign in. The token travels only in
// this request's body, through REST that logs no bodies; the error that
// comes back keeps only Discord's code, not the request.
func (m *Module) add(ctx context.Context, guild, user snowflake.ID, token string) error {
	if err := m.guard.Allow(guild, guard.MemberAdd); err != nil {
		return link.Err(link.Busy, "too many joins at once")
	}
	var mem *discord.Member // stays nil when they were already in (204)
	err := m.discord.Quiet().Do(rest.AddMember.Compile(nil, guild, user), discord.MemberAdd{AccessToken: token}, &mem, rest.WithCtx(ctx))
	m.guard.Report(guild, struggling(err))
	if re, ok := errors.AsType[*rest.Error](err); ok {
		out := &rest.Error{Code: re.Code, Message: re.Message}
		if re.Response != nil {
			out.Response = &http.Response{Status: re.Response.Status, StatusCode: re.Response.StatusCode}
		}
		return out
	}
	return err
}

// invite makes a Discord invite into g that only user can take, once,
// within five minutes, and waits for Discord to finish limiting it to them.
func (m *Module) invite(ctx context.Context, g discord.Guild, user snowflake.ID) (string, error) {
	ch, err := m.door(ctx, g)
	if err != nil {
		return "", err
	}
	if err := m.guard.Allow(g.ID, guard.InviteCreate); err != nil {
		return "", link.Err(link.Busy, "too many joins at once")
	}
	age, uses := inviteAge, 1
	inv, err := m.discord.Rest().CreateInvite(ch, discord.InviteCreate{MaxAge: &age, MaxUses: &uses, Unique: true, TargetUserIDs: []snowflake.ID{user}}, rest.WithCtx(ctx))
	m.guard.Report(g.ID, struggling(err))
	if err != nil {
		return "", err
	}
	for {
		job, err := m.discord.Rest().GetInviteTargetUsersJobStatus(inv.Code, rest.WithCtx(ctx))
		switch {
		case err != nil:
			return "", err
		case job.Status == discord.TargetUsersJobStatusCodeFailed:
			return "", fmt.Errorf("limiting invite to its one person failed")
		case job.Status != discord.TargetUsersJobStatusCodeProcessing:
			return inv.Code, nil
		}
		select {
		case <-ctx.Done():
			return "", link.Err(link.Timeout)
		case <-time.After(jobEvery):
		}
	}
}

// door is the channel an invite into g opens on: its rules, its system
// channel, or else its first text channel.
func (m *Module) door(ctx context.Context, g discord.Guild) (snowflake.ID, error) {
	for _, id := range []*snowflake.ID{g.RulesChannelID, g.SystemChannelID} {
		if id != nil && *id != 0 {
			return *id, nil
		}
	}
	chs, err := m.discord.Rest().GetGuildChannels(g.ID, rest.WithCtx(ctx))
	if err != nil {
		return 0, err
	}
	var best discord.GuildChannel
	for _, c := range chs {
		if c.Type() == discord.ChannelTypeGuildText && (best == nil || c.Position() < best.Position()) {
			best = c
		}
	}
	if best == nil {
		return 0, fmt.Errorf("%s has no text channel to invite into", g.ID)
	}
	return best.ID(), nil
}

// arrive records user joining guild, logging what fails: the sweep and the
// gateway event record it too, so a miss here is not a lost join.
func (m *Module) arrive(ctx context.Context, guild, user snowflake.ID, at time.Time) {
	if _, err := m.store.Arrive(ctx, guild, user, at); err != nil {
		m.log.Warn("recording a join", "guild", guild, "err", err)
	}
}

// read checks a call is the Worker's and decodes its body into v, and the
// code hash at code.
func read(caller string, body json.RawMessage, v any, code *string) ([]byte, error) {
	if caller != Edge {
		return nil, link.Err(link.Denied, "only cygnet.lol redeems codes")
	}
	if err := json.Unmarshal(body, v); err != nil {
		return nil, link.Err(link.Invalid, "body")
	}
	hash, err := hex.DecodeString(*code)
	if err != nil || len(hash) != 32 {
		return nil, link.Err(link.Invalid, "code is a SHA-256 in hex")
	}
	return hash, nil
}

// struggling is whether err says Discord is under strain (429, 5xx), which
// is all guard's breaker counts.
func struggling(err error) bool {
	re, ok := errors.AsType[*rest.Error](err)
	return ok && re.Response != nil && (re.Response.StatusCode == http.StatusTooManyRequests || re.Response.StatusCode >= 500)
}

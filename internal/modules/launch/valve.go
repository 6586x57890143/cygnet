package launch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/store"
	"github.com/6586x57890143/cygnet/link"
)

// The valve (SPEC.md, What an admin sees): once a launch is open, cygnet
// lets its line in by waves, each sized by how the server took the last.
// The rule is next, and every wave keeps what it read and why it came out
// that size, so a wave can be explained from its inputs (Principles 10).

const (
	// window is how far back joins a minute are read, and how long the
	// first wave is at the ceiling.
	window = 5 * time.Minute
	// settleBy is how long a wave has before the next may follow anyway.
	settleBy = 15 * time.Minute
	// most is the most minutes at the ceiling a wave can be.
	most = 20
)

// pace is skua's answer to skua.pace: messages a minute in the server now,
// and as usual. A question, never the messages (Principles 8).
type pace struct {
	Now   float64 `json:"now"`
	Usual float64 `json:"usual"`
}

// reading is everything a wave's size comes from.
type reading struct {
	ceiling int
	last    store.Wave
	// joins is the most joins a minute, over any window, since the last
	// wave opened: how the server took it at its busiest.
	joins float64
	// pace is nil when skua didn't say.
	pace *pace
}

// settled is whether the next wave may follow w: nine in ten of it are in
// the server, or joins have fallen under half the ceiling a window after
// it opened (its rush is over), or it has had settleBy. joins is joins a
// minute over the last window.
func settled(w store.Wave, joins float64, ceiling int, now time.Time) bool {
	return w.Arrived*10 >= w.Admitted*9 || now.Sub(w.Opened) >= window && joins < float64(ceiling)/2 || now.Sub(w.Opened) >= settleBy
}

// next is the next wave's size and why. The first is window's worth of
// joins at the ceiling. After that it doubles when joins stayed under half
// the ceiling the whole wave and the server talks no more than twice as
// much as usual, halves when joins went over the ceiling, and holds
// otherwise, between 1 and most minutes at the ceiling.
func next(r reading) (int, string) {
	c := r.ceiling
	if r.last.ID == 0 {
		n := c * int(window/time.Minute)
		return n, fmt.Sprintf("the first wave is %d: %d minutes of joins at the ceiling of %d a minute", n, int(window/time.Minute), c)
	}
	joins := fmt.Sprintf("joins peaked at %.1f a minute against a ceiling of %d", r.joins, c)
	talk, calm := "skua didn't say how busy the server is", true
	if r.pace != nil {
		calm = r.pace.Now <= 2*r.pace.Usual
		talk = fmt.Sprintf("messages ran at %.0f a minute against %.0f usual", r.pace.Now, r.pace.Usual)
	}
	size, verb := r.last.Size, "held at"
	switch {
	case r.joins > float64(c):
		size, verb = size/2, "halved to"
	case r.joins < float64(c)/2 && calm:
		size, verb = size*2, "doubled to"
	}
	size = min(max(size, 1), most*c)
	return size, fmt.Sprintf("%s %d: %s; %s", verb, size, joins, talk)
}

// joins counts who joined each server, from GUILD_MEMBER_ADD, for as long
// as a wave can run.
type joins struct {
	mu sync.Mutex
	at map[snowflake.ID][]time.Time
}

func (j *joins) add(guild snowflake.ID, now time.Time) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.at == nil {
		j.at = map[snowflake.ID][]time.Time{}
	}
	j.at[guild] = append(recent(j.at[guild], now), now)
}

// peak is the most joins a minute in guild over any window ending a whole
// minute after since, up to now.
func (j *joins) peak(guild snowflake.ID, since, now time.Time) float64 {
	j.mu.Lock()
	defer j.mu.Unlock()
	ts, ok := j.at[guild]
	if !ok {
		return 0
	}
	ts = recent(ts, now)
	j.at[guild] = ts
	most := 0
	for end := since.Add(time.Minute); ; end = end.Add(time.Minute) {
		if end.After(now) {
			end = now
		}
		n := 0
		for _, t := range ts {
			if !t.After(end) && end.Sub(t) <= window {
				n++
			}
		}
		most = max(most, n)
		if !end.Before(now) {
			break
		}
	}
	return float64(most) / window.Minutes()
}

// kept is how long joins are kept: as long as a wave can run, and a window
// before it.
const kept = settleBy + window

func recent(ts []time.Time, now time.Time) []time.Time {
	i := 0
	for i < len(ts) && now.Sub(ts[i]) > kept {
		i++
	}
	return ts[i:]
}

// Door lets someone into a server by a cause cygnet already holds: added
// with their own token, or a Discord invite only they can take.
// invite.Module's Let in production, wired in main.
type Door func(ctx context.Context, guild, user snowflake.ID, token string) (added bool, invite string, err error)

// Let gives the module its way into a server, and the app's OAuth secret,
// with which it refreshes a sealed sign in at its wave.
func (m *Module) Let(d Door, secret string) *Module {
	m.door, m.secret = d, secret
	return m
}

// flow turns the valve each minute, once cygnet is connected.
func (m *Module) flow() {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
		m.tick(ctx, m.now())
		cancel()
		time.Sleep(time.Minute)
	}
}

// tick turns every flowing launch's valve.
func (m *Module) tick(ctx context.Context, now time.Time) {
	if m.store == nil {
		return
	}
	ls, err := m.store.Flowing(ctx)
	if err != nil {
		m.log.Warn("reading launches to let in", "err", err)
	}
	for _, l := range ls {
		if err := m.turn(ctx, l, now); err != nil {
			m.log.Warn("letting a launch's line in", "launch", l.Name, "err", err)
		}
	}
}

// turn lets in whoever followed someone already in, opens the next wave
// once the last has settled, and adds the people cygnet adds herself.
func (m *Module) turn(ctx context.Context, l store.Launch, now time.Time) error {
	if _, err := m.store.Follow(ctx, l.Name, now); err != nil {
		return err
	}
	last, err := m.store.Last(ctx, l.Name)
	if err != nil {
		return err
	}
	if last.ID == 0 || !last.Settled.IsZero() || settled(last, m.joins.peak(l.Guild, now.Add(-time.Minute), now), l.Ceiling, now) {
		if last.ID != 0 && last.Settled.IsZero() {
			if err := m.store.Settle(ctx, last.ID, now); err != nil {
				return err
			}
		}
		size, why := next(reading{ceiling: l.Ceiling, last: last, joins: m.joins.peak(l.Guild, last.Opened, now), pace: m.pace(ctx, l.Guild)})
		if _, err := m.store.Admit(ctx, l.Name, size, why, now); err != nil {
			return err
		}
	}
	return m.addSealed(ctx, l, now)
}

// pace asks skua how busy guild is, or nil when it can't say.
func (m *Module) pace(ctx context.Context, guild snowflake.ID) *pace {
	if m.flock == nil || m.flock.fleet == nil {
		return nil
	}
	var p pace
	if err := m.flock.fleet.Call(ctx, "skua.pace", map[string]string{"guild": guild.String()}, &p); err != nil {
		return nil
	}
	return &p
}

// addSealed adds up to the ceiling's worth of people let in who left a
// sealed sign in: a minute's worth, each tick. One who can't be added gets
// their way in on their page instead.
func (m *Module) addSealed(ctx context.Context, l store.Launch, now time.Time) error {
	if m.opener == nil || m.door == nil {
		return nil
	}
	users, err := m.store.Sealed(ctx, l.Name, l.Ceiling)
	if err != nil {
		return err
	}
	for _, u := range users {
		if err := m.addOne(ctx, l, u, now); err != nil {
			m.log.Info("a sealed sign in didn't add its person; their page has their way in", "launch", l.Name, "err", err)
		}
	}
	return nil
}

// addOne opens user's sealed sign in, which takes it off the line,
// refreshes it, adds them with it, and gives the grant back.
func (m *Module) addOne(ctx context.Context, l store.Launch, user snowflake.ID, now time.Time) error {
	ct, err := m.store.Unseal(ctx, l.Name, user, now)
	if err != nil {
		return err
	}
	rt, err := m.opener.Open(ct)
	if err != nil {
		return err
	}
	token, err := m.oauth(ctx, "/oauth2/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {rt}})
	if err != nil {
		return err
	}
	defer func() {
		_, _ = m.oauth(context.WithoutCancel(ctx), "/oauth2/token/revoke", url.Values{"token": {token}, "token_type_hint": {"access_token"}})
	}()
	added, _, err := m.door(ctx, l.Guild, user, token)
	if err == nil && !added {
		err = errors.New("their sign in no longer adds them")
	}
	return err
}

// discordAPI is where the app's OAuth lives; a test points it elsewhere.
var discordAPI = "https://discord.com/api/v10"

// oauth posts form to one of the app's OAuth endpoints, as the app, and is
// the access token it answers with, if any. Nothing it sends or gets is
// logged.
func (m *Module) oauth(ctx context.Context, path string, form url.Values) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, discordAPI+path, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(m.app.String(), m.secret)
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", errors.New("discord's oauth didn't answer")
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("discord's oauth answered %d", res.StatusCode)
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	return out.AccessToken, nil
}

// admit is cygnet.admit: someone a wave let in comes for their way in,
// added with the token they just gave, or else an invite only they can
// take.
func (m *Module) admit(ctx context.Context, caller string, body json.RawMessage) (any, error) {
	var b struct {
		Token string `json:"token"`
	}
	a, user, err := m.read(caller, body)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(body, &b)
	p, err := m.store.Where(ctx, a.Launch, user, m.now())
	if err != nil {
		return nil, refused(err)
	}
	if p.Admitted.IsZero() {
		return nil, link.Err(link.Denied, NotYet)
	}
	l, err := m.store.Get(ctx, a.Launch)
	if err != nil {
		return nil, refused(err)
	}
	if m.door == nil {
		return nil, link.Err(link.Busy, "cygnet is starting")
	}
	added, inv, err := m.door(ctx, l.Guild, user, b.Token)
	if err != nil {
		return nil, err
	}
	return map[string]any{"guild": l.Guild.String(), "added": added, "invite": inv}, nil
}

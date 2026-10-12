package launch

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/hub"
	"github.com/6586x57890143/cygnet/link"
)

// Fleet is the hub as the flock on cygnet.lol reads it: who is linked, and
// a call to one of them for its manifest. Nil when the hub is off.
type Fleet interface {
	Fleet() []hub.Bird
	Call(ctx context.Context, method string, in, out any) error
}

// Users is what the flock reads from Discord: each bird's bot user, for its
// avatar.
type Users interface {
	GetUser(userID snowflake.ID, opts ...rest.RequestOpt) (*discord.User, error)
}

const (
	// look is how often cygnet looks for a bird that linked.
	look = time.Minute
	// fresh is how long a bird's picture is good for on cygnet.lol before
	// she sends it again; the Worker shows a bird seen in the last day.
	fresh = time.Hour
	// cells is the picture's size: a 128 px avatar in 4 px cells, for a
	// 14 px icon on a sharp screen.
	cells = 32
)

// flock is the birds cygnet sends to cygnet.lol for its footer, and when
// each was last sent.
type flock struct {
	fleet Fleet
	cdn   string
	mu    sync.Mutex
	sent  map[string]time.Time
	once  sync.Once
}

// Flock gives the module the hub, so cygnet.lol's footer shows every bird
// linked to her.
func (m *Module) Flock(f Fleet) *Module {
	m.flock = &flock{fleet: f, cdn: "https://cdn.discordapp.com", sent: map[string]time.Time{}}
	return m
}

// fly starts sending the flock once, for as long as cygnet runs.
//
// ponytail: a goroutine with no stop; the process ends it. A context
// from main if cygnet ever stops modules without exiting.
func (m *Module) fly(u Users, self snowflake.ID) {
	if m.flock == nil || m.edge == nil {
		return
	}
	m.flock.once.Do(func() {
		go func() {
			for {
				m.send(context.Background(), u, self, time.Now())
				time.Sleep(look)
			}
		}()
	})
}

// send sends each bird linked now whose picture cygnet.lol hasn't had
// within fresh: cygnet herself, then the hub's.
func (m *Module) send(ctx context.Context, u Users, self snowflake.ID, now time.Time) {
	f := m.flock
	birds := map[string]snowflake.ID{"cygnet": self}
	if f.fleet != nil {
		for _, b := range f.fleet.Fleet() {
			birds[b.Name] = 0
		}
	}
	for name, app := range birds {
		f.mu.Lock()
		due := now.Sub(f.sent[name]) >= fresh
		f.mu.Unlock()
		if !due {
			continue
		}
		if err := m.sendOne(ctx, u, name, app); err != nil {
			if m.log != nil {
				m.log.Warn("sending a bird to cygnet.lol", "bird", name, "err", err)
			}
			continue
		}
		f.mu.Lock()
		f.sent[name] = now
		f.mu.Unlock()
	}
}

var errNoAvatar = errors.New("the bird has no avatar")

// sendOne sends one bird's avatar as its picture. A bird's app is read from
// its manifest when it isn't known; its bot user's ID is the app's.
func (m *Module) sendOne(ctx context.Context, u Users, name string, app snowflake.ID) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if app == 0 {
		var mf link.Manifest
		if err := m.flock.fleet.Call(ctx, name+"."+link.ManifestMethod, nil, &mf); err != nil {
			return err
		}
		id, err := snowflake.Parse(mf.App)
		if err != nil {
			return fmt.Errorf("its manifest doesn't say which app it is: %w", err)
		}
		app = id
	}
	user, err := u.GetUser(app, rest.WithCtx(ctx))
	if err != nil {
		return err
	}
	if user.Avatar == nil {
		return errNoAvatar
	}
	pic, err := m.avatar(ctx, fmt.Sprintf("%s/avatars/%s/%s.png?size=128", m.flock.cdn, app, *user.Avatar))
	if err != nil {
		return err
	}
	return m.edge.do(ctx, http.MethodPut, "/admin/bird/"+name, map[string]string{"png": base64.StdEncoding.EncodeToString(pic)}, nil)
}

// avatar is the picture at url, pixelated to cells by cells.
func (m *Module) avatar(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	c := m.edge.Client
	if c == nil {
		c = http.DefaultClient
	}
	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discord's cdn answered %d", res.StatusCode)
	}
	img, err := png.Decode(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := png.Encode(&out, pixelate(img, cells)); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// pixelate is img in n by n cells, each the colour most of its pixels are
// (UX.md, Art: majority per cell, never one sample).
func pixelate(img image.Image, n int) *image.NRGBA {
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, n, n))
	for cy := range n {
		for cx := range n {
			votes := map[color.NRGBA]int{}
			var best color.NRGBA
			for y := b.Min.Y + cy*b.Dy()/n; y < b.Min.Y+(cy+1)*b.Dy()/n; y++ {
				for x := b.Min.X + cx*b.Dx()/n; x < b.Min.X+(cx+1)*b.Dx()/n; x++ {
					c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
					votes[c]++
					if votes[c] > votes[best] {
						best = c
					}
				}
			}
			out.SetNRGBA(cx, cy, best)
		}
	}
	return out
}

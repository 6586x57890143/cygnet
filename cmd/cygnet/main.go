// Command cygnet is the bot. Wiring only: everything with a decision in it
// lives in a package with a test.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	_ "net/http/pprof" // served only when CYGNET_PPROF names a loopback address
	"os"
	"os/signal"
	"slices"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/cache"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/brand"
	"github.com/6586x57890143/cygnet/internal/core"
	"github.com/6586x57890143/cygnet/internal/guard"
	"github.com/6586x57890143/cygnet/internal/hub"
	"github.com/6586x57890143/cygnet/internal/intents"
	"github.com/6586x57890143/cygnet/internal/modules/help"
	"github.com/6586x57890143/cygnet/internal/modules/invite"
	"github.com/6586x57890143/cygnet/internal/modules/launch"
	"github.com/6586x57890143/cygnet/internal/modules/perf"
	"github.com/6586x57890143/cygnet/internal/modules/provision"
	"github.com/6586x57890143/cygnet/internal/modules/status"
	"github.com/6586x57890143/cygnet/internal/obs"
	"github.com/6586x57890143/cygnet/internal/ratelimit"
	"github.com/6586x57890143/cygnet/internal/store"
	"github.com/6586x57890143/cygnet/link"
	"github.com/6586x57890143/cygnet/link/perms"
)

// reprobe is how often cygnet re-reads the portal's intent toggles. A change
// exits the process cleanly and compose's restart policy brings it back
// identifying with the new set.
//
// ponytail: restart rather than swapping the gateway connection in place;
// a reconnect inside the process is the upgrade if restarts ever hurt.
const reprobe = 10 * time.Minute

// userReset is whether a user-scope 429 is waited out to its bucket's reset
// rather than its retry_after; skua's tools/reactbench -compare policy
// decided it.
const userReset = true

var errIntentsChanged = errors.New("privileged intents changed in the Developer Portal; restarting to re-identify")

func main() {
	level := new(slog.LevelVar)
	_ = level.UnmarshalText([]byte(env("CYGNET_LOG_LEVEL", "info")))
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)

	err := run(log)
	switch {
	case errors.Is(err, errIntentsChanged):
		log.Info(err.Error())
	case err != nil:
		log.Error("cygnet stopped", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	token := os.Getenv("DISCORD_BOT_TOKEN")
	if token == "" {
		return errors.New("DISCORD_BOT_TOKEN is not set")
	}
	var bootstrap snowflake.ID
	if v := os.Getenv("CYGNET_BOOTSTRAP_ADMIN_USER_ID"); v != "" {
		id, err := snowflake.Parse(v)
		if err != nil {
			return fmt.Errorf("CYGNET_BOOTSTRAP_ADMIN_USER_ID: %w", err)
		}
		bootstrap = id
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := servePprof(os.Getenv("CYGNET_PPROF"), log); err != nil {
		return err
	}

	var db status.Pinger
	// Modules a guild has turned off; without a database a restart turns
	// them all back on.
	toggles := core.NewToggles(nil, nil)
	// The servers provision keeps to a layout; without a database a restart
	// forgets them, and their drift goes unreported until a layout is
	// applied again.
	kept := map[snowflake.ID]provision.Kept{}
	var saveKept func(context.Context, snowflake.ID, provision.Kept) error
	// Codes and joins live only in Postgres: without it there is no /invite.
	var codes *store.Codes
	if dsn := os.Getenv("CYGNET_DATABASE_URL"); dsn != "" {
		pool, err := store.Open(ctx, dsn)
		if err != nil {
			return err
		}
		defer pool.Close()
		if err := store.Migrate(ctx, pool); err != nil {
			return err
		}
		db = pool
		codes = store.NewCodes(pool)
		off, err := store.ModulesOff(ctx, pool)
		if err != nil {
			return fmt.Errorf("reading module switches: %w", err)
		}
		toggles = core.NewToggles(off, store.SaveModule(pool))
		all, err := store.AllProvisioned(ctx, pool)
		if err != nil {
			return fmt.Errorf("reading provisioned servers: %w", err)
		}
		for _, p := range all {
			kept[p.Guild] = provision.Kept{Layout: p.Layout, Mode: p.Mode, Leave: p.Leave}
		}
		save := store.SaveProvisioned(pool)
		saveKept = func(ctx context.Context, g snowflake.ID, k provision.Kept) error {
			return save(ctx, store.Provisioned{Guild: g, Layout: k.Layout, Mode: k.Mode, Leave: k.Leave})
		}
	}

	// Ask the portal what is granted before choosing what to identify with.
	probeRest := rest.New(rest.NewClient(token))
	app, err := probeRest.GetCurrentApplication()
	if err != nil {
		return fmt.Errorf("reading application flags: %w", err)
	}
	granted := intents.Granted(app.Flags)

	var client *bot.Client
	var probe status.Probe
	// Read by help once the guide is asked for, by which point both are set.
	var router *core.Router
	var running []core.Module
	// One guard for every writer: its breaker is per guild across modules.
	g := guard.New()
	st := status.New(func() status.Probe { return probe }, db, func() time.Duration {
		if client == nil || client.Gateway == nil {
			return 0
		}
		return client.Gateway.Latency()
	})
	// The hub runs only where birds are configured, and listens only on the
	// fleet's private network: never published, never behind a public name.
	// provision reads the fleet through it; a nil interface, not a nil *Hub,
	// when it is off.
	var fleet provision.Fleet
	// prov answers the birds' refusals once it exists; the hub listens
	// before the modules are made, so until then the answer is busy.
	var prov atomic.Pointer[provision.Module]
	if v := os.Getenv("CYGNET_BOTS"); v != "" {
		bots, err := hub.Parse(v)
		if err != nil {
			return err
		}
		h := hub.New(bots, log)
		_, verb, _ := link.Target(perms.MissingMethod)
		h.Serve(verb, func(ctx context.Context, caller string, body json.RawMessage) (any, error) {
			if p := prov.Load(); p != nil {
				return p.Missing(ctx, caller, body)
			}
			return nil, link.Err(link.Busy, "cygnet is starting")
		})
		fleet = h
		st.WithFleet(h.Fleet)
		srv := &http.Server{Addr: env("CYGNET_HUB_ADDR", ":7700"), Handler: h, ReadHeaderTimeout: 5 * time.Second}
		go func() {
			log.Info("hub listening", "addr", srv.Addr, "birds", len(bots))
			if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
				log.Error("hub stopped", "err", err)
			}
		}()
		// Birds hear goaway before the listener goes.
		defer func() { h.Close(); _ = srv.Shutdown(context.Background()) }()
	}
	prov.Store(provision.New(fleet, func() discord.Permissions { return core.Install(running).Permissions }, func(i discord.Interaction) bool { return router.Admin(i) }, g).Keep(kept, saveKept, log).Live(func() bool { return probe.Identified.Has(gateway.IntentGuildMembers) }))
	all := []core.Module{
		st,
		perf.New(obs.Default),
		prov.Load(),
		launch.New(edge(), func(i discord.Interaction) bool { return router.Admin(i) }, log).Flock(fleet),
	}
	if codes != nil {
		all = append(all, invite.New(codes, members{func() *bot.Client { return client }}, os.Getenv("CYGNET_EDGE_URL"), func(i discord.Interaction) bool { return router.Admin(i) }))
	} else {
		log.Warn("no database, so no /invite: codes live in Postgres")
	}
	all = append(all, help.New(func() []core.Module { return running }, func(i discord.Interaction) bool { return router.Admin(i) }, toggles))

	wants := make(map[string]intents.Want, len(all))
	for _, m := range all {
		wants[m.Name()] = m.Want()
	}
	identify, skipped := intents.Resolve(wants, granted)
	probe = status.Probe{Granted: granted, Identified: identify, Skipped: skipped}
	for _, name := range skipped {
		log.Warn("module skipped: a required intent is not granted", "module", name)
	}

	router = core.NewRouter(bootstrap, func(g snowflake.ID) (snowflake.ID, bool) {
		guild, ok := client.Caches.Guild(g)
		return guild.OwnerID, ok
	}, log)
	router.Gate(toggles)
	// Gateway events reach the running modules that listen for them.
	var listeners []bot.EventListener
	for _, m := range all {
		if slices.Contains(skipped, m.Name()) {
			continue
		}
		if err := router.Add(m); err != nil {
			return err
		}
		running = append(running, m)
		if gm, ok := m.(core.Gated); ok {
			name := m.Name()
			gm.Gate(func(guild snowflake.ID) bool { return toggles.On(guild, name) })
		}
		if l, ok := m.(bot.EventListener); ok {
			listeners = append(listeners, toggles.Listen(m.Name(), obs.Listen(obs.Default, m.Name(), l)))
		}
	}
	// Icons go up once as application emoji, so no reply re-uploads one.
	// Off the boot path: until it finishes, or if it fails, they go as
	// attachments, as before.
	go func() {
		ctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		if err := brand.Sync(ctx, probeRest, app.ID, g); err != nil {
			log.Warn("syncing emoji; the icons missing go as attachments", "err", err)
		}
	}()
	// The install link asks for what the running modules declare, so it
	// follows every module added, removed or skipped. A failure only leaves
	// the link stale, which is no reason not to boot.
	install := core.Install(running)
	syncInstall := func(user bool) {
		if wrote, err := core.SyncInstall(probeRest, app, install, user); err != nil {
			log.Warn("updating the install settings", "err", err)
		} else if wrote {
			log.Info("install settings updated", "permissions", install.Permissions, "user", user)
		}
	}
	// The commands a user installs live in the global list, the rest in
	// each guild's, which no longer has them: so a failed write is tried
	// again until it lands. Overwritten whole, so one dropped from the code
	// goes. Discord lists a user installed command only while user install
	// is on, so it is switched on before they are listed and off after.
	global := router.Global()
	setGlobal := func() {
		for {
			_, err := probeRest.SetGlobalCommands(app.ID, global)
			if err == nil {
				return
			}
			log.Warn("registering the global commands; trying again in a minute", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Minute):
			}
		}
	}
	if len(global) > 0 {
		syncInstall(true)
		go setGlobal()
	} else {
		go func() { setGlobal(); syncInstall(false) }()
	}

	register := func(c *bot.Client, guild snowflake.ID) {
		if _, err := c.Rest.SetGuildCommands(c.ApplicationID, guild, router.CreatesFor(guild)); err != nil {
			log.Warn("registering commands", "guild", guild, "err", err)
		}
	}
	// A module turned off or on leaves or joins the guild's command list,
	// within the guild's command budget (a user installed command, being
	// global, stays listed, and dispatch refuses it). Over budget, it tries
	// again once the budget has room: dispatch refuses an off module
	// meanwhile, but a module turned back on would otherwise stay missing
	// from the menu.
	var resync func(snowflake.ID)
	resync = core.Coalesce(func(guild snowflake.ID) {
		if err := g.Allow(guild, guard.CommandSync); err != nil {
			time.AfterFunc(guard.Step(guard.CommandSync), func() { resync(guild) })
			return
		}
		_, err := client.Rest.SetGuildCommands(client.ApplicationID, guild, router.CreatesFor(guild))
		g.Report(guild, err != nil)
		if err != nil {
			log.Warn("re-registering commands after a switch", "guild", guild, "err", err)
		}
	})
	toggles.OnChange = resync

	client, err = disgo.New(token,
		bot.WithLogger(log),
		// Every REST call's rate limit wait and round trip, for /perf, and a
		// route's 429 waited out to the millisecond rather than the second.
		bot.WithRestClientConfigOpts(rest.WithRateLimiter(obs.Limiter(ratelimit.Precise(rest.NewRateLimiter(rest.WithRateLimiterLogger(log)), userReset), obs.Default))),
		bot.WithGatewayConfigOpts(
			gateway.WithIntents(identify),
			gateway.WithPresenceOpts(gateway.WithCustomActivity(brand.StatusAt(0))),
		),
		// Only the guild cache is read (owner lookup); everything else off.
		bot.WithCacheConfigOpts(cache.WithCaches(cache.FlagGuilds)),
		bot.WithEventListenerFunc(router.OnCommand),
		bot.WithEventListenerFunc(router.OnModal),
		bot.WithEventListenerFunc(router.OnComponent),
		bot.WithEventListeners(listeners...),
		bot.WithEventListenerFunc(func(e *events.GuildReady) { register(e.Client(), e.GuildID) }),
		bot.WithEventListenerFunc(func(e *events.GuildJoin) { register(e.Client(), e.GuildID) }),
	)
	if err != nil {
		return err
	}
	defer client.Close(context.Background())

	if err := client.OpenGateway(ctx); err != nil {
		return fmt.Errorf("opening gateway: %w", err)
	}
	log.Info("cygnet is up", "modules", len(all)-len(skipped), "intents", identify)

	line := 0
	tick := time.NewTicker(reprobe)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			// The status line rides the re-probe tick: no loop of its own.
			line++
			if err := client.SetPresence(ctx, gateway.WithCustomActivity(brand.StatusAt(line))); err != nil {
				log.Warn("setting the status line", "err", err)
			}
			app, err := probeRest.GetCurrentApplication()
			if err != nil {
				log.Warn("re-probing intents", "err", err)
				continue
			}
			if intents.Granted(app.Flags) != granted {
				return errIntentsChanged
			}
		}
	}
}

// servePprof refuses anything but a loopback address: a profiler on a
// public port is a heap dump for whoever asks.
func servePprof(addr string, log *slog.Logger) error {
	if addr == "" {
		return nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("CYGNET_PPROF: %w", err)
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("CYGNET_PPROF must be a loopback address, got %q", addr)
	}
	// The flight recorder rides the same loopback port: the last seconds of
	// execution trace, for go tool trace, with a region per module.
	flight, err := obs.Flight()
	if err != nil {
		return fmt.Errorf("CYGNET_PPROF: flight recorder: %w", err)
	}
	http.Handle("/debug/cygnet/flight", flight)
	go func() {
		log.Info("pprof listening", "addr", addr, "flight", "/debug/cygnet/flight")
		log.Warn("pprof stopped", "err", http.ListenAndServe(addr, nil)) //nolint:gosec // loopback only, checked above
	}()
	return nil
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// members is Discord as /invite reads it: the servers in the guild cache,
// and whether someone is in one, asked of Discord when they ask.
type members struct{ client func() *bot.Client }

func (m members) Guilds() []discord.Guild { return slices.Collect(m.client().Caches.Guilds()) }

func (m members) Member(ctx context.Context, guild, user snowflake.ID) (bool, error) {
	_, err := m.client().Rest.GetMember(guild, user, rest.WithCtx(ctx))
	if rest.IsJSONErrorCode(err, rest.JSONErrorCodeUnknownMember) {
		return false, nil
	}
	return err == nil, err
}

// edge is the Worker at cygnet.lol that /launch registers with,
// if CYGNET_EDGE_URL and CYGNET_EDGE_TOKEN (the Worker's ADMIN_TOKEN) are set.
func edge() *launch.Edge {
	u, t := os.Getenv("CYGNET_EDGE_URL"), os.Getenv("CYGNET_EDGE_TOKEN")
	if u == "" || t == "" {
		return nil
	}
	e := &launch.Edge{URL: u, Token: t}
	if id, err := snowflake.Parse(os.Getenv("CYGNET_BOOTSTRAP_ADMIN_USER_ID")); err == nil {
		e.Breakglass = id
	}
	return e
}

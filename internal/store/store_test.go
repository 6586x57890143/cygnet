package store

import (
	"context"
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/6586x57890143/cygnet/internal/core"
)

// The test needs a real Postgres: CI runs one, and locally `docker compose up
// postgres` with CYGNET_TEST_DATABASE_URL pointing at it. Unset, it skips.
func dsn(t testing.TB) string {
	t.Helper()
	d := os.Getenv("CYGNET_TEST_DATABASE_URL")
	if d == "" {
		t.Skip("CYGNET_TEST_DATABASE_URL is not set")
	}
	return d
}

func TestMigrateTwiceIsANoop(t *testing.T) {
	ctx := context.Background()
	pool, err := Open(ctx, dsn(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for range 2 {
		if err := Migrate(ctx, pool); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := pool.QueryRow(ctx, "select count(*) from schema_migrations where version = 'migrations/001_module_off.sql'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("001 recorded %d times, want 1", n)
	}
	if _, err := pool.Exec(ctx, "select guild_id, module from module_off limit 0"); err != nil {
		t.Fatalf("module_off: %v", err)
	}
}

func TestOpenRefusesABadDSN(t *testing.T) {
	if _, err := Open(context.Background(), "postgres://nobody@127.0.0.1:1/x?connect_timeout=1"); err == nil {
		t.Fatal("Open succeeded against a closed port")
	}
	if _, err := Open(context.Background(), "::not a dsn"); err == nil {
		t.Fatal("Open accepted a malformed DSN")
	}
}

// A migration that fails takes the ones before it in the same run down with
// it: the schema is either all the files or none of the new ones.
func TestMigrateRollsBackAFailure(t *testing.T) {
	ctx := context.Background()
	pool, err := Open(ctx, dsn(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	defer func(m fs.FS) { migrations = m }(migrations)
	migrations = fstest.MapFS{
		"migrations/900_good.sql": {Data: []byte("create table rollback_probe (id int)")},
		"migrations/901_bad.sql":  {Data: []byte("this is not sql")},
	}
	if err := Migrate(ctx, pool); err == nil {
		t.Fatal("a broken migration applied")
	}
	var exists bool
	if err := pool.QueryRow(ctx, "select to_regclass('rollback_probe') is not null").Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("900_good.sql survived 901_bad.sql failing")
	}
}

func TestModuleSwitchesRoundTrip(t *testing.T) {
	ctx := context.Background()
	pool, err := Open(ctx, dsn(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	save := SaveModule(pool)
	s := core.Switch{Guild: 1 << 60, Module: "perf"}
	for _, on := range []bool{false, false} {
		if err := save(ctx, s, on); err != nil {
			t.Fatal(err)
		}
	}
	off, err := ModulesOff(ctx, pool)
	if err != nil || !slices.Contains(off, s) {
		t.Fatalf("off %v (%v), want %v in it", off, err, s)
	}
	if err := save(ctx, s, true); err != nil {
		t.Fatal(err)
	}
	if off, _ := ModulesOff(ctx, pool); slices.Contains(off, s) {
		t.Fatal("turned on, still off")
	}
}

func TestProvisioned(t *testing.T) {
	ctx := context.Background()
	pool, err := Open(ctx, dsn(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "delete from provision where guild_id in (91, 92)"); err != nil {
		t.Fatal(err)
	}
	save := SaveProvisioned(pool)
	for _, p := range []Provisioned{{91, "flock", "propose", nil}, {92, "flock", "watch", nil}, {91, "flock", "act", []string{"! role  mod: colour"}}} {
		if err := save(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	if save(ctx, Provisioned{93, "flock", "loud", nil}) == nil {
		t.Fatal("a mode that isn't one was kept")
	}
	all, err := AllProvisioned(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64]string{}
	for _, p := range all {
		got[int64(p.Guild)] = p.Layout + "/" + p.Mode + "/" + strings.Join(p.Leave, ",")
	}
	if got[91] != "flock/act/! role  mod: colour" || got[92] != "flock/watch/" {
		t.Fatalf("got %v", got)
	}
}

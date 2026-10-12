package core

import (
	"errors"
	"testing"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
)

type permsMod struct {
	mod
	p discord.Permissions
}

func (m permsMod) Perms() discord.Permissions { return m.p }

func TestInstallIsTheUnionOfRunningModules(t *testing.T) {
	got := Install([]Module{permsMod{p: discord.PermissionViewChannel}, permsMod{p: discord.PermissionManageWebhooks}, permsMod{}})
	if got.Permissions != discord.PermissionViewChannel|discord.PermissionManageWebhooks {
		t.Errorf("permissions = %d", got.Permissions)
	}
	if !sameInstall(got, discord.InstallParams{Scopes: []discord.OAuth2Scope{discord.OAuth2ScopeBot, discord.OAuth2ScopeApplicationsCommands}, Permissions: got.Permissions}) {
		t.Errorf("scopes = %v, want bot and applications.commands", got.Scopes)
	}
}

type appRest struct {
	rest.Rest
	updates []discord.ApplicationUpdate
	err     error
}

func (a *appRest) UpdateCurrentApplication(u discord.ApplicationUpdate, _ ...rest.RequestOpt) (*discord.Application, error) {
	a.updates = append(a.updates, u)
	return &discord.Application{}, a.err
}

func TestSyncInstallWritesOnlyWhenDiscordDiffers(t *testing.T) {
	want := Install([]Module{permsMod{p: discord.PermissionManageWebhooks}})
	guild, user := discord.ApplicationIntegrationTypeGuildInstall, discord.ApplicationIntegrationTypeUserInstall
	in := func(p discord.InstallParams) discord.ApplicationIntegrationTypeConfiguration {
		return discord.ApplicationIntegrationTypeConfiguration{OAuth2InstallParams: &p}
	}
	reordered := want
	reordered.Scopes = []discord.OAuth2Scope{discord.OAuth2ScopeBot, discord.OAuth2ScopeApplicationsCommands}
	fewer := want
	fewer.Permissions = 0
	commands := discord.InstallParams{Scopes: []discord.OAuth2Scope{discord.OAuth2ScopeApplicationsCommands}}

	cases := []struct {
		name  string
		have  discord.ApplicationIntegrationTypesConfig
		write bool
	}{
		{"nothing set (a new app)", nil, true},
		{"guild entry without params", discord.ApplicationIntegrationTypesConfig{guild: {}}, true},
		{"stale permissions", discord.ApplicationIntegrationTypesConfig{guild: in(fewer)}, true},
		{"user install still on", discord.ApplicationIntegrationTypesConfig{guild: in(want), user: in(commands)}, true},
		{"already right", discord.ApplicationIntegrationTypesConfig{guild: in(want)}, false},
		{"already right, scopes in another order", discord.ApplicationIntegrationTypesConfig{guild: in(reordered)}, false},
	}
	for _, c := range cases {
		r := &appRest{}
		wrote, err := SyncInstall(r, &discord.Application{IntegrationTypesConfig: c.have}, want, false)
		if err != nil || wrote != c.write || len(r.updates) != map[bool]int{false: 0, true: 1}[c.write] {
			t.Errorf("%s: wrote=%v err=%v updates=%d", c.name, wrote, err, len(r.updates))
			continue
		}
		if c.write {
			cfg := *r.updates[0].IntegrationTypesConfig
			if len(cfg) != 1 || cfg[guild].OAuth2InstallParams == nil || !sameInstall(*cfg[guild].OAuth2InstallParams, want) {
				t.Errorf("%s: sent %+v, want guild only with %+v", c.name, cfg, want)
			}
		}
	}

	r := &appRest{err: errors.New("503")}
	if wrote, err := SyncInstall(r, &discord.Application{}, want, false); err == nil || wrote {
		t.Errorf("a failed write: wrote=%v err=%v", wrote, err)
	}
}

func TestUserInstallFollowsTheCommands(t *testing.T) {
	user := discord.ApplicationIntegrationTypeUserInstall
	slash := Command{Create: discord.SlashCommandCreate{Name: "invite", IntegrationTypes: []discord.ApplicationIntegrationType{discord.ApplicationIntegrationTypeGuildInstall, user}}, Tier: Public, Run: run}
	guildOnly := Command{Create: discord.SlashCommandCreate{Name: "launch"}, Tier: Admin, Run: run}
	message := Command{Create: discord.MessageCommandCreate{Name: "pin", IntegrationTypes: []discord.ApplicationIntegrationType{user}}, Tier: Public, Run: run}
	if UserInstall([]Module{mod{[]Command{guildOnly}}}) || !UserInstall([]Module{mod{[]Command{guildOnly}}, mod{[]Command{slash}}}) {
		t.Fatal("UserInstall doesn't follow the commands")
	}
	if !UserInstalled(message.Create) || UserInstalled(discord.UserCommandCreate{Name: "who"}) {
		t.Fatal("UserInstalled reads the wrong types")
	}

	r := NewRouter(0, nil, nil)
	r.Gate(NewToggles(nil, nil))
	if err := r.Add(mod{[]Command{slash, guildOnly, message}}); err != nil {
		t.Fatal(err)
	}
	if g, all := r.Global(), r.CreatesFor(1); len(g) != 2 || len(all) != 1 || all[0].CommandName() != "launch" {
		t.Fatalf("global %d, guild %v", len(g), all)
	}

	want := Install(nil)
	a := &appRest{}
	if wrote, err := SyncInstall(a, &discord.Application{}, want, true); !wrote || err != nil {
		t.Fatalf("wrote=%v err=%v", wrote, err)
	}
	cfg := *a.updates[0].IntegrationTypesConfig
	if p := cfg[user].OAuth2InstallParams; p == nil || len(p.Scopes) != 1 || p.Scopes[0] != discord.OAuth2ScopeApplicationsCommands || p.Permissions != 0 {
		t.Fatalf("user install %+v", cfg[user])
	}
	// As Discord has it now, nothing to write.
	if wrote, _ := SyncInstall(a, &discord.Application{IntegrationTypesConfig: cfg}, want, true); wrote {
		t.Fatal("wrote what Discord already had")
	}
}

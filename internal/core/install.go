package core

import (
	"slices"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
)

// Install is what adding cygnet to a server asks for: the bot and its slash
// commands, with exactly the permissions the running modules declare. A
// module skipped for a missing intent asks for nothing.
func Install(running []Module) discord.InstallParams {
	var p discord.Permissions
	for _, m := range running {
		p |= m.Perms()
	}
	return discord.InstallParams{
		Scopes:      []discord.OAuth2Scope{discord.OAuth2ScopeApplicationsCommands, discord.OAuth2ScopeBot},
		Permissions: p,
	}
}

// SyncInstall makes want the app's Default Install Settings, which is what
// Discord's bare install link (and cygnet.lol, which redirects to it)
// asks a server for, and switches user install on (asking for commands
// alone) exactly when user is set. It writes only when Discord's copy
// differs, so a restart costs one comparison, and reports whether it wrote.
func SyncInstall(r rest.Applications, app *discord.Application, want discord.InstallParams, user bool) (bool, error) {
	guild, userType := discord.ApplicationIntegrationTypeGuildInstall, discord.ApplicationIntegrationTypeUserInstall
	cfg := discord.ApplicationIntegrationTypesConfig{guild: {OAuth2InstallParams: &want}}
	if user {
		cfg[userType] = discord.ApplicationIntegrationTypeConfiguration{OAuth2InstallParams: &discord.InstallParams{Scopes: []discord.OAuth2Scope{discord.OAuth2ScopeApplicationsCommands}}}
	}
	if sameConfig(app.IntegrationTypesConfig, cfg) {
		return false, nil
	}
	_, err := r.UpdateCurrentApplication(discord.ApplicationUpdate{IntegrationTypesConfig: &cfg})
	return err == nil, err
}

func sameConfig(have, want discord.ApplicationIntegrationTypesConfig) bool {
	if len(have) != len(want) {
		return false
	}
	for t, w := range want {
		h, ok := have[t]
		if !ok || h.OAuth2InstallParams == nil || !sameInstall(*h.OAuth2InstallParams, *w.OAuth2InstallParams) {
			return false
		}
	}
	return true
}

func sameInstall(a, b discord.InstallParams) bool {
	if a.Permissions != b.Permissions || len(a.Scopes) != len(b.Scopes) {
		return false
	}
	x, y := slices.Clone(a.Scopes), slices.Clone(b.Scopes)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}

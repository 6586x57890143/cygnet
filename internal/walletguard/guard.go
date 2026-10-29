// Package walletguard checks the pinned release's offline wallet_info output
// before the wallet service opens a listener. It never asks for secret keys.
package walletguard

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/6586x57890143/cygnet/internal/chain"
)

const (
	Wallet   = "/wallet/support-view"
	Password = "/run/secrets/xmr-wallet-password"
	Config   = "/run/secrets/xmr-wallet.conf"
)

// Check runs one fixed, offline, informational CLI command. Output is never
// forwarded to service logs. Duplicate fields fail closed, including fields
// injected into the wallet's free-form description.
func Check(ctx context.Context, primary, network string, run func(context.Context, []string) ([]byte, error)) error {
	actual, sub, valid := chain.MoneroAddress(primary)
	if !valid || sub || actual != network {
		return errors.New("wallet guard: invalid pinned primary address or network")
	}
	args := []string{"--offline", "--wallet-file", Wallet, "--password-file", Password, "--log-file", "/tmp/wallet-preflight.log", "--log-level", "0"}
	if network == "Stagenet" {
		args = append(args, "--stagenet")
	}
	args = append(args, "wallet_info")
	raw, err := run(ctx, args)
	if err != nil || len(raw) > 65536 {
		return errors.New("wallet guard: could not inspect wallet")
	}
	want := map[string]string{"Address": primary, "Type": "Watch only", "Network type": network}
	seen := map[string]bool{}
	for line := range strings.SplitSeq(string(raw), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ": ")
		if !ok {
			continue
		}
		if expected, ok := want[key]; ok {
			if seen[key] || value != expected {
				return errors.New("wallet guard: wallet must be the pinned view-only wallet")
			}
			seen[key] = true
		}
	}
	if len(seen) != len(want) {
		return errors.New("wallet guard: incomplete wallet information")
	}
	return nil
}

// ValidateConfig binds the checked file to the file RPC will open, requires
// TLS for both RPC hops, and refuses options that can disable authentication
// or certificate checks. Configuration contains secrets; errors include none.
func ValidateConfig(raw []byte, network string) error {
	if len(raw) == 0 || len(raw) > 65536 || (network != "Mainnet" && network != "Stagenet") {
		return errors.New("wallet guard: invalid service configuration")
	}
	allowed := map[string]bool{"wallet-file": true, "password-file": true, "rpc-bind-ip": true, "rpc-bind-port": true,
		"confirm-external-bind": true, "rpc-login": true, "rpc-ssl": true, "rpc-ssl-private-key": true,
		"rpc-ssl-certificate": true, "daemon-address": true, "daemon-login": true, "daemon-ssl": true,
		"daemon-ssl-ca-certificates": true, "daemon-ssl-allow-chained": true,
		"untrusted-daemon": true, "log-file": true, "log-level": true, "max-log-file-size": true, "max-log-files": true, "stagenet": true}
	values := map[string]string{}
	for line := range strings.SplitSeq(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || !allowed[key] || values[key] != "" || value == "" {
			return errors.New("wallet guard: unsupported or duplicate configuration option")
		}
		values[key] = value
	}
	for key, expected := range map[string]string{"wallet-file": Wallet, "password-file": Password, "rpc-bind-ip": "0.0.0.0",
		"rpc-bind-port": "18088", "confirm-external-bind": "1", "rpc-ssl": "enabled", "daemon-ssl": "enabled",
		"daemon-ssl-ca-certificates": "/etc/ssl/certs/ca-certificates.crt", "daemon-ssl-allow-chained": "1",
		"rpc-ssl-private-key": "/run/secrets/xmr-rpc-tls.key", "rpc-ssl-certificate": "/run/secrets/xmr-rpc-ca.crt",
		"untrusted-daemon": "1", "log-file": "/tmp/wallet-rpc.log", "log-level": "0", "max-log-file-size": "1048576", "max-log-files": "1"} {
		if values[key] != expected {
			return errors.New("wallet guard: required service protection is missing")
		}
	}
	user, password, ok := strings.Cut(values["rpc-login"], ":")
	if !ok || user == "" || len(password) < 32 {
		return errors.New("wallet guard: RPC requires a long authentication secret")
	}
	node, err := url.Parse(values["daemon-address"])
	if err != nil || node.Scheme != "https" || node.Hostname() == "" || node.User != nil || node.RawQuery != "" || node.Fragment != "" {
		return errors.New("wallet guard: daemon requires a TLS endpoint")
	}
	if (network == "Stagenet" && values["stagenet"] != "1") || (network == "Mainnet" && values["stagenet"] != "") {
		return errors.New("wallet guard: configured network differs from pinned network")
	}
	return nil
}

package walletguard

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const primary = "45Ps9mEFLZQ5hqrFVMsH7gXvydddfW5EnA185A2oQL69SzSVKTGxXAGebDaWqs2uNSUYmGoQMTLeT9QPxtwfXEh45nSwxz1"

func TestGuardUsesOnlyOfflineInformationAndRejectsSpendEnabledWallets(t *testing.T) {
	info := "Filename: /wallet/support-view\nDescription: fixture\nAddress: " + primary + "\nType: Watch only\nNetwork type: Mainnet\n"
	run := func(_ context.Context, args []string) ([]byte, error) {
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "--offline --wallet-file "+Wallet+" --password-file "+Password) || args[len(args)-1] != "wallet_info" || strings.Contains(joined, "viewkey") || strings.Contains(joined, "spendkey") {
			t.Fatal("guard may inspect only offline wallet metadata")
		}
		return []byte(info), nil
	}
	if err := Check(context.Background(), primary, "Mainnet", run); err != nil {
		t.Fatal(err)
	}
	valid := info
	for _, bad := range []string{
		strings.ReplaceAll(valid, "Type: Watch only", "Type: Normal"),
		strings.ReplaceAll(valid, "Type: Watch only", "Type: Background wallet"),
		strings.ReplaceAll(valid, "Type: Watch only", "Type: 2/3 multisig"),
		strings.ReplaceAll(valid, "Network type: Mainnet", "Network type: Stagenet"),
		strings.ReplaceAll(valid, "Address: "+primary, "Address: another-wallet"),
		strings.ReplaceAll(valid, "Type: Watch only", ""),
		valid + "Type: Watch only\n", valid + "Type: Normal\n", "", strings.Repeat("x", 65537),
	} {
		info = bad
		if err := Check(context.Background(), primary, "Mainnet", run); err == nil {
			t.Fatal("accepted ambiguous or unsafe wallet information")
		}
	}
	if err := Check(context.Background(), "invalid", "Mainnet", run); err == nil {
		t.Fatal("invalid pinned wallet accepted")
	}
	if err := Check(context.Background(), primary, "Stagenet", run); err == nil {
		t.Fatal("mismatched network accepted")
	}
	if err := Check(context.Background(), primary, "Mainnet", func(context.Context, []string) ([]byte, error) { return nil, errors.New("private diagnostic") }); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal("inspection errors must be sanitized")
	}
}

const configuration = `# public fixture with a disposable password
wallet-file=/wallet/support-view
password-file=/run/secrets/xmr-wallet-password
rpc-bind-ip=0.0.0.0
rpc-bind-port=18088
confirm-external-bind=1
rpc-login=fixture:abcdefghijklmnopqrstuvwxyz1234567890
rpc-ssl=enabled
rpc-ssl-private-key=/run/secrets/xmr-rpc-tls.key
rpc-ssl-certificate=/run/secrets/xmr-rpc-ca.crt
daemon-address=https://node.example:18081
daemon-ssl=enabled
daemon-ssl-ca-certificates=/etc/ssl/certs/ca-certificates.crt
daemon-ssl-allow-chained=1
untrusted-daemon=1
log-file=/tmp/wallet-rpc.log
log-level=0
max-log-file-size=1048576
max-log-files=1
`

func TestGuardBindsTheInspectedWalletAndRequiresAuthenticatedTLS(t *testing.T) {
	if err := ValidateConfig([]byte(configuration), "Mainnet"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateConfig([]byte(configuration+"stagenet=1\n"), "Stagenet"); err != nil {
		t.Fatal(err)
	}
	for _, replacement := range [][2]string{
		{"wallet-file=/wallet/support-view", "wallet-file=/wallet/hot"},
		{"password-file=/run/secrets/xmr-wallet-password", "password-file=/other"},
		{"rpc-ssl=enabled", "rpc-ssl=disabled"},
		{"daemon-ssl=enabled", "daemon-ssl=disabled"},
		{"untrusted-daemon=1", "untrusted-daemon=0"},
		{"rpc-login=fixture:abcdefghijklmnopqrstuvwxyz1234567890", "rpc-login=fixture:short"},
		{"rpc-login=fixture:abcdefghijklmnopqrstuvwxyz1234567890", "rpc-login=missing-colon"},
		{"rpc-login=fixture:abcdefghijklmnopqrstuvwxyz1234567890", "rpc-login=:abcdefghijklmnopqrstuvwxyz1234567890"},
		{"https://node.example:18081", "http://node.example:18081"},
		{"https://node.example:18081", "https://user:secret@node.example:18081"},
		{"https://node.example:18081", "https://node.example:18081?secret=yes"},
		{"https://node.example:18081", "https://node.example:18081#secret"},
		{"https://node.example:18081", "https://"},
		{"https://node.example:18081", "://bad"},
	} {
		if err := ValidateConfig([]byte(strings.ReplaceAll(configuration, replacement[0], replacement[1])), "Mainnet"); err == nil {
			t.Fatalf("accepted unsafe option %s", replacement[0])
		}
	}
	for _, raw := range []string{"", strings.Repeat("x", 65537), configuration + "disable-rpc-login=1\n", configuration + "rpc-ssl-allow-any-cert=1\n", configuration + "rpc-login=private\n", configuration + "[section]\n", configuration + "daemon-login=\n", configuration + "stagenet=1\n"} {
		if err := ValidateConfig([]byte(raw), "Mainnet"); err == nil || strings.Contains(err.Error(), "private") {
			t.Fatal("accepted invalid configuration or exposed its contents")
		}
	}
	if ValidateConfig([]byte(configuration), "Stagenet") == nil || ValidateConfig([]byte(configuration), "Testnet") == nil {
		t.Fatal("network mismatch accepted")
	}
}

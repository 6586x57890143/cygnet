// monero-check verifies the private wallet using the same client as cygnet.
// It reads the bot's file-mounted RPC credentials; it never opens wallet files.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/6586x57890143/cygnet/internal/chain"
)

func run(ctx context.Context) error {
	password, err := readFile(os.Getenv("CYGNET_XMR_RPC_PASSWORD_FILE"), 1024)
	if err != nil {
		return errors.New("wallet check: cannot read RPC password file")
	}
	certificate, err := readFile(os.Getenv("CYGNET_XMR_RPC_CA_FILE"), 65536)
	if err != nil {
		return errors.New("wallet check: cannot read pinned certificate")
	}
	wallet, err := chain.NewMonero(os.Getenv("CYGNET_XMR_RPC"), os.Getenv("CYGNET_SUPPORT_MONERO"),
		os.Getenv("CYGNET_XMR_NETWORK"), os.Getenv("CYGNET_XMR_RPC_USER"), strings.TrimSpace(string(password)), certificate)
	if err != nil {
		return err
	}
	return wallet.Check(ctx, time.Now())
}

func readFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || len(raw) == 0 || int64(len(raw)) > limit {
		return nil, errors.New("invalid file size or unreadable file")
	}
	return raw, nil
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := run(ctx); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Wallet identity, verified TLS, RPC authentication and synchronization passed.")
}

//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/6586x57890143/cygnet/internal/walletguard"
)

type boundedOutput struct{ bytes.Buffer }

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 65536 {
		return 0, errors.New("wallet output exceeds limit")
	}
	return b.Buffer.Write(p)
}

func main() {
	if err := start(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func start() error {
	if len(os.Args) != 1 {
		return errors.New("wallet guard: command arguments are not accepted")
	}
	network := os.Getenv("CYGNET_XMR_NETWORK")
	if network == "" {
		network = "Mainnet"
	}
	f, err := os.Open(walletguard.Config)
	if err != nil {
		return errors.New("wallet guard: service configuration is unavailable")
	}
	raw, err := io.ReadAll(io.LimitReader(f, 65537))
	_ = f.Close()
	if err != nil {
		return errors.New("wallet guard: cannot read service configuration")
	}
	if err := walletguard.ValidateConfig(raw, network); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	environment := []string{"HOME=/tmp", "TMPDIR=/tmp", "LANG=C", "LC_ALL=C", "NO_COLOR=1"}
	err = walletguard.Check(ctx, os.Getenv("CYGNET_XMR_PRIMARY"), network, func(ctx context.Context, args []string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "/usr/local/bin/monero-wallet-cli", args...)
		cmd.Env = environment
		var output boundedOutput
		cmd.Stdout, cmd.Stderr = &output, io.Discard
		cmd.WaitDelay = time.Second
		err := cmd.Run()
		return output.Bytes(), err
	})
	if err != nil {
		return err
	}
	return syscall.Exec("/usr/local/bin/monero-wallet-rpc", []string{"monero-wallet-rpc", "--config-file", walletguard.Config}, environment)
}

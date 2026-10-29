// native-check validates private discovery endpoints and independent RPC reads.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/6586x57890143/cygnet/internal/chain"
)

func run(ctx context.Context) error {
	endpoints, err := chain.LoadNativeEndpoints(os.Getenv("CYGNET_NATIVE_FILE"))
	if err != nil {
		return err
	}
	r := &chain.Reader{Native: endpoints, RPC: map[string]string{"base": os.Getenv("CYGNET_RPC_BASE"), "ethereum": os.Getenv("CYGNET_RPC_ETHEREUM")}}
	for _, rail := range chain.Rails {
		if !rail.Native || endpoints[rail.Chain] == "" {
			continue
		}
		// Public canonical WETH contracts provide real native-transfer samples
		// without querying the owner's receiving history or moving any funds.
		to := map[string]string{"base": "0x4200000000000000000000000000000000000006", "ethereum": "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"}[rail.Chain]
		_, cursor, err := r.EVMNative(ctx, rail, to, "")
		if err != nil {
			return fmt.Errorf("%s: %w", rail.Chain, err)
		}
		next, err := strconv.ParseUint(cursor, 10, 64)
		if err != nil || next < 2 {
			return errors.New("native check: invalid initial scan boundary")
		}
		got, _, err := r.EVMNative(ctx, rail, to, strconv.FormatUint(max(uint64(1), next-16), 10))
		if err != nil {
			return fmt.Errorf("%s: %w", rail.Chain, err)
		}
		if len(got) == 0 {
			return fmt.Errorf("%s: no recent public transfer sample was available", rail.Chain)
		}
		internal := 0
		for _, transfer := range got {
			if strings.Contains(transfer.Event, ":internal") {
				internal++
			}
		}
		fmt.Printf("%s: %d real native receipts validated (%d internal), including finalized RPC checks.\n", rail.Chain, len(got), internal)
	}
	assets := []string{"ETH"}
	if os.Getenv("CYGNET_XMR_PRICE_CHECK") == "1" {
		assets = append(assets, "XMR")
	}
	for _, asset := range assets {
		if _, err := r.USD(ctx, asset); err != nil {
			return fmt.Errorf("%s: live quote price unavailable", asset)
		}
		fmt.Printf("%s: live quote price passed validation.\n", asset)
	}
	return nil
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := run(ctx); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// native-check validates private discovery endpoints and independent RPC reads.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
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
		// A public burn address avoids querying the owner's receiving history.
		const to = "0x000000000000000000000000000000000000dEaD"
		_, cursor, err := r.EVMNative(ctx, rail, to, "")
		if err != nil {
			return fmt.Errorf("%s: %w", rail.Chain, err)
		}
		next, err := strconv.ParseUint(cursor, 10, 64)
		if err != nil || next < 2 {
			return errors.New("native check: invalid initial scan boundary")
		}
		if _, _, err := r.EVMNative(ctx, rail, to, strconv.FormatUint(next-1, 10)); err != nil {
			return fmt.Errorf("%s: %w", rail.Chain, err)
		}
		fmt.Printf("%s: Cygnet native discovery and finalized RPC reads passed.\n", rail.Chain)
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

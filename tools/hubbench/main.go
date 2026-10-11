// Command hubbench times cygnet.ping through the live hub: a warmup, then
// -n calls one at a time, then -n more with -par in flight, printing p50,
// p90, p99, max and the rate for each. It links as the bird `bench`, a name
// of its own in CYGNET_BOTS, since a second link on a running bird's token
// replaces that bird's link. On the host, from a checkout:
//
//	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -o hubbench ./tools/hubbench
//	docker run --rm --network fleet --read-only --cap-drop ALL --user "$(id -u):$(id -g)" //	  -v "$PWD/hubbench:/hubbench:ro" -v ~/cygnet/bench.token:/token:ro //	  gcr.io/distroless/static-debian12:nonroot /hubbench
//
// The token is read from the file -token names and never printed.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/6586x57890143/cygnet/link"
)

func main() {
	url := flag.String("url", "ws://cygnet:7700/v1/link", "")
	tokFile := flag.String("token", "/token", "")
	n := flag.Int("n", 5000, "calls per phase")
	par := flag.Int("par", 8, "in flight in the second phase")
	flag.Parse()
	tok, err := os.ReadFile(*tokFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "token:", err)
		os.Exit(1)
	}
	c := link.New(link.Config{URL: *url, Token: strings.TrimSpace(string(tok)), Build: "hubbench"})
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = c.Run(ctx); close(done) }()
	for !c.Linked() {
		time.Sleep(50 * time.Millisecond)
	}
	ping := func() (time.Duration, error) {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		t := time.Now()
		err := c.Call(cctx, "cygnet.ping", nil, nil)
		return time.Since(t), err
	}
	for range 200 {
		_, _ = ping()
	}
	for _, p := range []int{1, *par} {
		ds := make([]time.Duration, 0, *n)
		var mu sync.Mutex
		var wg sync.WaitGroup
		var fails int
		jobs := make(chan struct{})
		start := time.Now()
		for range p {
			wg.Go(func() {
				for range jobs {
					d, err := ping()
					mu.Lock()
					if err != nil {
						fails++
					} else {
						ds = append(ds, d)
					}
					mu.Unlock()
				}
			})
		}
		for range *n {
			jobs <- struct{}{}
		}
		close(jobs)
		wg.Wait()
		wall := time.Since(start)
		slices.Sort(ds)
		q := func(f float64) time.Duration { return ds[int(f*float64(len(ds)-1))] }
		fmt.Printf("inflight=%d ok=%d fail=%d p50=%v p90=%v p99=%v max=%v rate=%.0f/s\n",
			p, len(ds), fails, q(.5), q(.9), q(.99), ds[len(ds)-1], float64(len(ds))/wall.Seconds())
	}
	stop()
	<-done
}

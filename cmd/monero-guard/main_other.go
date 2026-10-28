//go:build !linux

package main

import (
	"fmt"
	"os"
)

func main() {
	_, _ = fmt.Fprintln(os.Stderr, "monero-guard runs only inside the Linux wallet image")
	os.Exit(1)
}

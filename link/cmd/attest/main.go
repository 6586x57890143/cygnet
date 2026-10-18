// Command attest signs a bird's build for cygnet, in the bird's CI.
//
//	go run github.com/6586x57890143/cygnet/link/cmd/attest@<link version> -new
//
// makes a release key: the private half (BIRD_RELEASE_KEY) goes into the
// bird's repository as a secret of its deploy job on main and nowhere
// else, the public half into cygnet's CYGNET_BIRD_KEYS as name=key.
//
//	BIRD_RELEASE_KEY=... attest -bird skua -commit <sha> -image sha256:<digest>
//
// prints the attestation, one line, for the bird's environment
// (SKUA_ATTEST, MERLIN_ATTEST): what it shows cygnet at hello.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/6586x57890143/cygnet/link"
)

func main() {
	if err := run(os.Args[1:], os.Getenv("BIRD_RELEASE_KEY"), time.Now()); err != nil {
		fmt.Fprintln(os.Stderr, "✗", err)
		os.Exit(1)
	}
}

func run(args []string, key string, now time.Time) error {
	fs := flag.NewFlagSet("attest", flag.ContinueOnError)
	fresh := fs.Bool("new", false, "make a release key")
	bird := fs.String("bird", "", "the bird's name on cygnet's link")
	commit := fs.String("commit", "", "the full commit the image was built from")
	image := fs.String("image", "", "the image digest, sha256:...")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *fresh {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		fmt.Println("BIRD_RELEASE_KEY (the bird's CI secret):", base64.StdEncoding.EncodeToString(priv.Seed()))
		fmt.Println("CYGNET_BIRD_KEYS entry (cygnet's env):   <bird>=" + base64.StdEncoding.EncodeToString(pub))
		return nil
	}
	seed, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(seed) != ed25519.SeedSize {
		return errors.New("BIRD_RELEASE_KEY isn't a base64 Ed25519 seed (attest -new makes one)")
	}
	a := link.Sign(link.Attestation{Bird: *bird, Commit: *commit, Image: *image, Signed: now.Unix()}, ed25519.NewKeyFromSeed(seed))
	if err := a.Verify(*bird, ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey), now); err != nil {
		return fmt.Errorf("that doesn't make a good attestation: %w", err)
	}
	fmt.Println(a.Encode())
	return nil
}

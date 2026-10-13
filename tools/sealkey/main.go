// Command sealkey makes the key pair a launch's sealed sign ins use
// (SPEC.md, Sealed until launch): the private half for cygnet's
// CYGNET_SEAL_KEY, the public half for the Worker's CYGNET_SEAL_PUBLIC.
// Both are base64url. Replacing the pair leaves every token sealed to the
// old one unopenable: those people get their way in on their page instead.
package main

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

func main() {
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	enc := base64.RawURLEncoding.EncodeToString
	fmt.Printf("CYGNET_SEAL_KEY=%s\nCYGNET_SEAL_PUBLIC=%s\n", enc(k.Bytes()), enc(k.PublicKey().Bytes()))
}

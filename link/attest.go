package link

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// A bird proves its build to cygnet with an Attestation: its name, the
// commit and the image its CI built, signed with that bird's release key.
// The key lives only in the bird's own CI, as a secret of the deploy job on
// its main branch, so a fork (which never gets a repository's secrets) or a
// build from anywhere else can't sign one. cygnet pins each bird's public
// key and refuses a pinned bird's link without a good one; and where it can
// see the container the link comes from, it checks that container runs the
// very image that was signed.

// Trust is how far cygnet has checked the bird a call or event comes from.
// The hub stamps it on every frame it forwards, over whatever a bird wrote.
type Trust string

const (
	// Untrusted is a bird known by its token alone.
	Untrusted Trust = ""
	// Signed is a bird whose hello carried a good attestation from its
	// pinned release key.
	Signed Trust = "signed"
	// Attested is a signed bird that cygnet saw running the image the
	// attestation names.
	Attested Trust = "attested"
)

// AtLeast reports whether t is as trusted as want or more.
func (t Trust) AtLeast(want Trust) bool { return rank(t) >= rank(want) }

func rank(t Trust) int {
	switch t {
	case Attested:
		return 2
	case Signed:
		return 1
	}
	return 0
}

// Attestation is a bird's signed account of its build.
type Attestation struct {
	Bird   string `json:"bird"`
	Commit string `json:"commit"`          // the full 40 character commit
	Image  string `json:"image,omitempty"` // the image digest, sha256:...
	Signed int64  `json:"signed"`          // when CI signed it, Unix seconds
	Sig    []byte `json:"sig"`
}

// message is what the signature covers: every field, in order, under a
// label so the bytes can't be taken for anything else a key signs.
func (a Attestation) message() []byte {
	return []byte(strings.Join([]string{"cygnet attestation v1", a.Bird, a.Commit, a.Image, strconv.FormatInt(a.Signed, 10)}, "\n"))
}

// Sign is a with its signature from key.
func Sign(a Attestation, key ed25519.PrivateKey) Attestation {
	a.Sig = ed25519.Sign(key, a.message())
	return a
}

var (
	errShape  = errors.New("link: attestation isn't for this bird, or isn't well formed")
	errSig    = errors.New("link: attestation signature doesn't verify")
	errFuture = errors.New("link: attestation is signed in the future")
)

// Verify checks a is bird's, well formed, signed by key and not dated
// ahead of now.
func (a Attestation) Verify(bird string, key ed25519.PublicKey, now time.Time) error {
	if a.Bird != bird || !hex40(a.Commit) || (a.Image != "" && !digest(a.Image)) || a.Signed <= 0 {
		return errShape
	}
	if len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, a.message(), a.Sig) {
		return errSig
	}
	if time.Unix(a.Signed, 0).After(now.Add(5 * time.Minute)) {
		return errFuture
	}
	return nil
}

// Encode is a as one line, for an environment variable.
func (a Attestation) Encode() string {
	b, _ := json.Marshal(a)
	return base64.RawURLEncoding.EncodeToString(b)
}

// DecodeAttestation reads what Encode wrote.
func DecodeAttestation(s string) (Attestation, error) {
	var a Attestation
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return a, err
	}
	return a, json.Unmarshal(b, &a)
}

func hex40(s string) bool {
	return len(s) == 40 && strings.Trim(s, "0123456789abcdef") == ""
}

func digest(s string) bool {
	h, ok := strings.CutPrefix(s, "sha256:")
	return ok && len(h) == 64 && strings.Trim(h, "0123456789abcdef") == ""
}

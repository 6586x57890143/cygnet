package launch

import (
	"context"
	"crypto/ecdh"
	"crypto/hpke"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/6586x57890143/cygnet/link"
)

// Sealed until launch (SPEC.md, Membership): someone who asks to be added
// when their launch opens leaves a Discord refresh token, which cygnet.lol
// encrypts to cygnet's public key (edge/src/seal.ts) and can't open again.
// cygnet keeps it sealed, opens it at their wave, and forgets it.

// sealInfo binds a sealed token to what it's for, as the Worker seals it.
var sealInfo = []byte("cygnet.lol sealed sign in v1")

// Opener opens what cygnet.lol sealed to cygnet: HPKE (RFC 9180), base
// mode, DHKEM(X25519, HKDF-SHA256), HKDF-SHA256, AES-128-GCM.
type Opener struct{ key hpke.PrivateKey }

// NewOpener is the opener for a private key as CYGNET_SEAL_KEY holds it:
// 32 bytes of X25519, base64url.
func NewOpener(key string) (*Opener, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(key, "="))
	if err != nil {
		return nil, fmt.Errorf("seal key: %w", err)
	}
	k, err := hpke.DHKEM(ecdh.X25519()).NewPrivateKey(raw)
	if err != nil {
		return nil, fmt.Errorf("seal key: %w", err)
	}
	return &Opener{key: k}, nil
}

// Open is the token sealed in ct.
func (o *Opener) Open(ct []byte) (string, error) {
	pt, err := hpke.Open(o.key, hpke.HKDFSHA256(), hpke.AES128GCM(), sealInfo, ct)
	return string(pt), err
}

// Seal gives the module the key it opens sealed sign ins with; without one
// nobody can ask to be added when their launch opens.
func (m *Module) Seal(o *Opener) *Module {
	m.opener = o
	return m
}

// seal is cygnet.seal: keep user's sealed sign in (base64url), checked by
// opening it once, or with none, forget the one they left.
func (m *Module) seal(ctx context.Context, caller string, body json.RawMessage) (any, error) {
	var b struct {
		Sealed string `json:"sealed"`
	}
	a, user, err := m.read(caller, body)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(body, &b)
	var ct []byte
	if b.Sealed != "" {
		if m.opener == nil {
			return nil, link.Err(link.Denied, NoSeal)
		}
		if ct, err = base64.RawURLEncoding.DecodeString(b.Sealed); err != nil {
			return nil, link.Err(link.Invalid, "sealed is base64url")
		}
		if _, err := m.opener.Open(ct); err != nil {
			return nil, link.Err(link.Invalid, "sealed doesn't open")
		}
	}
	if err := m.store.Seal(ctx, a.Launch, user, ct, m.now()); err != nil {
		return nil, refused(err)
	}
	return map[string]bool{"sealed": ct != nil}, nil
}

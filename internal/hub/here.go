package hub

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/6586x57890143/cygnet/link"
)

// HereVerb is cygnet.here: whether cygnet is a member of a server. A bird
// that would do more for an attested bird where cygnet keeps watch (merlin
// granting a role without its admins listing it) asks her.
const HereVerb = "here"

// Here answers cygnet.here from in, which reports whether cygnet is in a
// server: {"guild": "<id>"} in, {"here": true|false} out. It says nothing
// about any server beyond yes or no.
func Here(in func(guild string) bool) Handler {
	return func(_ context.Context, _ string, body json.RawMessage) (any, error) {
		var q struct {
			Guild string `json:"guild"`
		}
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&q); err != nil || len(q.Guild) < 17 || len(q.Guild) > 20 || notDigits(q.Guild) {
			return nil, link.Err(link.Invalid, "body")
		}
		return map[string]bool{"here": in(q.Guild)}, nil
	}
}

// notDigits reports whether s holds anything but digits.
func notDigits(s string) bool { return len(bytes.Trim([]byte(s), "0123456789")) > 0 }

package hub

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/6586x57890143/cygnet/link"
)

// cygnet.here says yes or no for a server ID and refuses anything else.
func TestHere(t *testing.T) {
	fn := Here(func(g string) bool { return g == "100000000000000001" })
	for body, want := range map[string]bool{`{"guild":"100000000000000001"}`: true, `{"guild":"100000000000000002"}`: false} {
		got, err := fn(context.Background(), "merlin", json.RawMessage(body))
		if err != nil || got.(map[string]bool)["here"] != want {
			t.Errorf("%s: %v %v", body, got, err)
		}
	}
	for _, bad := range []string{`{}`, `{"guild":"12"}`, `{"guild":"10000000000000000x"}`, `{"guild":"100000000000000001","more":1}`, `nope`} {
		if _, err := fn(context.Background(), "merlin", json.RawMessage(bad)); !errors.Is(err, link.Err(link.Invalid)) {
			t.Errorf("%s: %v", bad, err)
		}
	}
}

package hub

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/6586x57890143/cygnet/link"
)

// A module answers a call to cygnet herself: it hears who called and with
// what, its answer is the reply, and its errors keep their codes, or are
// internal with the words kept here.
func TestServe(t *testing.T) {
	h := New(tokens, slog.New(slog.DiscardHandler))
	h.Serve("echo", func(_ context.Context, caller string, body json.RawMessage) (any, error) {
		var in struct{ Say string }
		_ = json.Unmarshal(body, &in)
		switch in.Say {
		case "busy":
			return nil, link.Err(link.Busy, "later")
		case "boom":
			return nil, errors.New("a reason that stays here")
		}
		return map[string]string{"from": caller, "said": in.Say}, nil
	})
	srv := httptest.NewServer(h)
	t.Cleanup(func() { h.Close(); srv.Close() })
	skua := bird(t, srv.URL, "skua", nil)
	linked(t, h, "skua")

	var out map[string]string
	if err := skua.Call(context.Background(), "cygnet.echo", map[string]string{"say": "hi"}, &out); err != nil || out["from"] != "skua" || out["said"] != "hi" {
		t.Fatalf("%v, %v", out, err)
	}
	code(t, skua.Call(context.Background(), "cygnet.echo", map[string]string{"say": "busy"}, nil), link.Busy)
	code(t, skua.Call(context.Background(), "cygnet.echo", map[string]string{"say": "boom"}, nil), link.Internal)
	// The built in verbs still answer beside it.
	if err := skua.Call(context.Background(), "cygnet.ping", nil, nil); err != nil {
		t.Fatal(err)
	}
}

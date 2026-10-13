package launch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"

	"github.com/6586x57890143/cygnet/link"
)

// ask is the Worker calling verb with body, its answer as JSON.
func ask(t *testing.T, m *Module, verb string, body map[string]any) (map[string]any, error) {
	t.Helper()
	b, _ := json.Marshal(body)
	out, err := m.Verbs()[verb](context.Background(), edgeBird, b)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(out)
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	return got, nil
}

// denied is whether err is a refusal for why.
func denied(err error, why string) bool {
	le, ok := errors.AsType[*link.Error](err)
	return ok && le.Code == link.Denied && le.Reason == why
}

func code(s string) string {
	h := sha256.Sum256([]byte("launch verbs " + s))
	return hex.EncodeToString(h[:])
}

func TestVerbs(t *testing.T) {
	m, _ := running(t)
	ctx := context.Background()
	if err := m.store.Register(ctx, "silcord", guild, t0); err != nil {
		t.Fatal(err)
	}
	l := map[string]any{"launch": "silcord"}
	with := func(kv ...any) map[string]any {
		out := map[string]any{"launch": "silcord"}
		for i := 0; i < len(kv); i += 2 {
			out[kv[i].(string)] = kv[i+1]
		}
		return out
	}

	got, err := ask(t, m, "join", with("user", "11"))
	if err != nil || got["place"] != 1.0 {
		t.Fatalf("join: %v %v", got, err)
	}
	if _, err := ask(t, m, "join", with("user", "11")); !denied(err, OnList) {
		t.Fatalf("join twice: %v", err)
	}
	if _, err := ask(t, m, "join", with("user", "12", "held", "same connection")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.store.Release(ctx, "silcord", 1, 2, owner, t0); err != nil {
		t.Fatal(err)
	}

	// An invite: made, shown on the giver's page, taken by someone new,
	// taken again by them (a retry), refused to anyone else.
	got, err = ask(t, m, "issue", with("user", "11", "code", code("a")))
	if err != nil || got["expires"] != float64(t0.Add(inviteLife).UnixMilli()) {
		t.Fatalf("issue: %v %v", got, err)
	}
	if _, err := ask(t, m, "issue", with("user", "11", "code", code("b"))); err != nil {
		t.Fatal(err)
	}
	if _, err := ask(t, m, "issue", with("user", "11", "code", code("c"))); !denied(err, NoneLeft) {
		t.Fatalf("a third of 2: %v", err)
	}
	for range 2 {
		got, err = ask(t, m, "vouch", map[string]any{"user": "13", "code": code("a")})
		if err != nil || got["launch"] != "silcord" {
			t.Fatalf("vouch: %v %v", got, err)
		}
	}
	if _, err := ask(t, m, "vouch", map[string]any{"user": "14", "code": code("a")}); !denied(err, Gone) {
		t.Fatalf("a spent invite: %v", err)
	}
	got, err = ask(t, m, "revoke", with("user", "11", "code", code("b")))
	if err != nil || got["revoked"] != true {
		t.Fatalf("revoke: %v %v", got, err)
	}

	got, err = ask(t, m, "place", with("user", "11"))
	if err != nil || got["rank"] != 1.0 || got["left"] != 1.0 || len(got["invites"].([]any)) != 2 || got["sponsored"] != false {
		t.Fatalf("the giver's place: %v %v", got, err)
	}
	got, _ = ask(t, m, "place", with("user", "13"))
	if got["sponsored"] != true || got["rank"] != 0.0 {
		t.Fatalf("the invitee's place: %v", got)
	}
	if _, err := ask(t, m, "place", with("user", "99")); !denied(err, NotOnList) {
		t.Fatalf("place of nobody: %v", err)
	}

	got, err = ask(t, m, "mine", with("user", "11"))
	if err != nil || got["discord_id"] != "11" || len(got["ledger"].([]any)) != 6 || len(got["invites_made"].([]any)) != 2 {
		t.Fatalf("mine: %v %v", got, err)
	}
	if _, err := ask(t, m, "mine", with("user", "99")); !denied(err, NotOnList) {
		t.Fatalf("mine of nobody: %v", err)
	}

	// The list and the lift, for whoever runs it: the server's owner and the
	// breakglass admin.
	if _, err := ask(t, m, "list", with("user", "11")); !denied(err, NotYours) {
		t.Fatalf("list for a member: %v", err)
	}
	got, err = ask(t, m, "lift", with("user", "9", "member", "12"))
	if err != nil || got["lifted"] != true {
		t.Fatalf("lift: %v %v", got, err)
	}
	if _, err := ask(t, m, "lift", with("user", "5", "member", "x")); err == nil {
		t.Fatal("a lift for no one")
	}
	if _, err := ask(t, m, "list", map[string]any{"launch": "nope", "user": "5"}); !denied(err, NoLaunch) {
		t.Fatalf("list of nothing: %v", err)
	}

	// The giver leaves: the invitee stays, under someone gone.
	if _, err := ask(t, m, "leave", with("user", "11")); err != nil {
		t.Fatal(err)
	}
	got, err = ask(t, m, "list", with("user", "5"))
	rows := got["rows"].([]any)
	if err != nil || len(rows) != 2 || rows[1].(map[string]any)["sponsor"] != "gone" {
		t.Fatalf("list after a leave: %v %v", got, err)
	}

	// Who may ask, and how.
	b, _ := json.Marshal(l)
	if _, err := m.join(ctx, "skua", b); !errors.Is(err, link.Err(link.Denied)) {
		t.Fatalf("another bird: %v", err)
	}
	if _, err := m.join(ctx, edgeBird, []byte("{")); !errors.Is(err, link.Err(link.Invalid)) {
		t.Fatalf("a bad body: %v", err)
	}
	if _, err := ask(t, m, "join", with("user", "x")); !errors.Is(err, link.Err(link.Invalid)) {
		t.Fatalf("a bad user: %v", err)
	}
	for _, v := range []string{"issue", "revoke", "vouch"} {
		if _, err := ask(t, m, v, with("user", "11", "code", "zz")); !errors.Is(err, link.Err(link.Invalid)) {
			t.Fatalf("%s with a bad code: %v", v, err)
		}
	}
	if _, err := module(nil).join(ctx, edgeBird, b); !errors.Is(err, link.Err(link.Denied)) {
		t.Fatalf("no database: %v", err)
	}
	if refused(errors.New("x")).Error() != "x" {
		t.Fatal("an error that isn't a refusal")
	}
}

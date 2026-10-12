package perms

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/messages"):
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"message": "Missing Permissions", "code": 50013}`)
		case strings.HasSuffix(r.URL.Path, "/webhooks"):
			// Forbidden for another reason: not a missing permission.
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"message": "Missing Access", "code": 50001}`)
		case strings.HasSuffix(r.URL.Path, "/pins/1"):
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `not json`)
		default:
			_, _ = io.WriteString(w, `{}`)
		}
	}))
	defer srv.Close()
	var got []Missing
	c := &http.Client{Transport: Watch(nil, func(m Missing) { got = append(got, m) })}
	do := func(method, path string) string {
		req, _ := http.NewRequest(method, srv.URL+"/api/v10"+path, nil)
		res, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		b, _ := io.ReadAll(res.Body)
		return string(b)
	}

	// A missing permission is reported, and the caller still reads the
	// refusal whole.
	if body := do("POST", "/channels/42/messages"); !strings.Contains(body, "50013") {
		t.Fatalf("body %q", body)
	}
	if len(got) != 1 || got[0] != (Missing{Channel: "42", Op: MessageSend}) {
		t.Fatalf("reported %+v", got)
	}
	// Anything else is left alone.
	do("POST", "/channels/42/webhooks")
	do("PUT", "/channels/42/pins/1")
	do("GET", "/channels/42")
	if len(got) != 1 {
		t.Fatalf("reported %+v", got)
	}

	// A transport that fails, fails as it did.
	broken := &http.Client{Transport: Watch(failing{}, func(Missing) { t.Error("reported a failure") })}
	if _, err := broken.Get(srv.URL); err == nil {
		t.Error("no error")
	}
	// A body that can't be read still reaches the caller.
	short := &http.Client{Transport: Watch(shortBody{}, func(Missing) { t.Error("reported an unreadable body") })}
	if res, err := short.Get(srv.URL + "/channels/42/messages"); err != nil || res.StatusCode != http.StatusForbidden {
		t.Errorf("%v %v", res, err)
	}
}

type failing struct{}

func (failing) RoundTrip(*http.Request) (*http.Response, error) { return nil, errors.New("down") }

type shortBody struct{}

func (shortBody) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(errReader{})}, nil
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("cut off") }

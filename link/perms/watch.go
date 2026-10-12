package perms

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

// missingPermissions is Discord's JSON error code for a request refused for
// want of a permission.
const missingPermissions = 50013

// Watch is base that notices, on every response, Discord refusing a
// request for want of a permission, and hands report what the request
// needed, read by Route. It changes no response: the caller still gets the
// refusal. A bird puts it under its Discord library's HTTP client once,
// and every refusal from then on is reported, whatever made it.
//
// report runs on the request's goroutine, so it should hand the Missing
// off (a call to cygnet) rather than wait on it.
func Watch(base http.RoundTripper, report func(Missing)) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return watch{base, report}
}

type watch struct {
	base   http.RoundTripper
	report func(Missing)
}

func (w watch) RoundTrip(req *http.Request) (*http.Response, error) {
	res, err := w.base.RoundTrip(req)
	if err != nil || res.StatusCode != http.StatusForbidden {
		return res, err
	}
	// Read the error to see its code, then give the caller the same body.
	body, rerr := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	_ = res.Body.Close()
	res.Body = io.NopCloser(bytes.NewReader(body))
	if rerr != nil {
		return res, nil
	}
	var e struct {
		Code int `json:"code"`
	}
	if json.Unmarshal(body, &e) == nil && e.Code == missingPermissions {
		if m, ok := Route(req.Method, req.URL.Path); ok {
			w.report(m)
		}
	}
	return res, nil
}

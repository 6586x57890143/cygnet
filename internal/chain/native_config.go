package chain

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
)

var alchemyEndpoint = map[string]*regexp.Regexp{
	"ethereum": regexp.MustCompile(`^https://eth-mainnet\.g\.alchemy\.com/v2/[A-Za-z0-9_-]{1,256}$`),
	"base":     regexp.MustCompile(`^https://base-mainnet\.g\.alchemy\.com/v2/[A-Za-z0-9_-]{1,256}$`),
}

// LoadNativeEndpoints reads the private JSON file prepared by the operator.
// File paths, endpoint values and parser errors never enter returned errors.
func LoadNativeEndpoints(path string) (map[string]string, error) {
	fail := errors.New("native: missing or invalid endpoint secret file")
	file, err := os.Open(path)
	if err != nil {
		return nil, fail
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, fail
	}
	raw, err := io.ReadAll(io.LimitReader(file, 8193))
	if err != nil || len(raw) == 0 || len(raw) > 8192 {
		return nil, fail
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, fail
	}
	values := map[string]string{}
	for decoder.More() {
		token, err := decoder.Token()
		name, ok := token.(string)
		pattern := alchemyEndpoint[name]
		if err != nil || !ok || pattern == nil || values[name] != "" {
			return nil, fail
		}
		var endpoint string
		if decoder.Decode(&endpoint) != nil || !pattern.MatchString(endpoint) {
			return nil, fail
		}
		values[name] = endpoint
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') || len(values) == 0 {
		return nil, fail
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fail
	}
	return values, nil
}

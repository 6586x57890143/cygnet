package support

import (
	"bytes"
	"image/png"
	"testing"
)

func TestSharedIconsAreCompleteAndDecodable(t *testing.T) {
	seen := map[string]bool{}
	for _, key := range Icons {
		if seen[key] {
			t.Fatalf("duplicate icon %q", key)
		}
		seen[key] = true
		raw, ok := IconPNG(key)
		if !ok {
			t.Fatalf("missing icon %q", key)
		}
		img, err := png.Decode(bytes.NewReader(raw))
		if err != nil || img.Bounds().Dx() != 128 || img.Bounds().Dy() != 128 {
			t.Fatalf("invalid icon %q: %v", key, err)
		}
	}
	for alias, key := range map[string]string{"ethereum": IconETH, "monero": IconXMR, "solana": IconSOL} {
		a, ok := IconPNG(alias)
		b, _ := IconPNG(key)
		if !ok || !bytes.Equal(a, b) {
			t.Errorf("%s must reuse %s", alias, key)
		}
	}
	for _, key := range []string{"", "../badge", "icons/badge", "unknown", "ETH"} {
		if _, ok := IconPNG(key); ok {
			t.Errorf("accepted unknown key %q", key)
		}
	}
}

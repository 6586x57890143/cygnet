package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"
)

// attest refuses a missing key and a bad build, and signs a good one.
func TestRun(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	key := base64.StdEncoding.EncodeToString(priv.Seed())
	commit := "0123456789abcdef0123456789abcdef01234567"
	if err := run([]string{"-new"}, "", now); err != nil {
		t.Errorf("-new: %v", err)
	}
	if err := run([]string{"-bird", "skua", "-commit", commit}, "", now); err == nil {
		t.Error("signed with no key")
	}
	if err := run([]string{"-bird", "skua", "-commit", "abc"}, key, now); err == nil {
		t.Error("signed a short commit")
	}
	if err := run([]string{"-bird", "skua", "-commit", commit, "-image", "sha256:" + commit + commit[:24]}, key, now); err != nil {
		t.Errorf("a good build: %v", err)
	}
	if err := run([]string{"-nope"}, key, now); err == nil {
		t.Error("an unknown flag")
	}
}

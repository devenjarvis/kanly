package main

import (
	"fmt"
	"os"
	"testing"
)

// TestMain points Kanly's caches at a fresh directory, so runs neither read
// nor fill the user's cache. Tests about the caches set their own.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "kanly-cache-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("KANLY_CACHE_DIR", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// Package testutil loads the frozen golden fixtures from testdata/golden.
// See testdata/README.md.
package testutil

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// GoldenDir returns the absolute path of testdata/golden.
func GoldenDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", "golden")
}

// Golden reads a fixture by its path relative to testdata/golden.
func Golden(t testing.TB, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(GoldenDir(), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read golden %s: %v", rel, err)
	}
	return data
}

// GoldenString is Golden as a string.
func GoldenString(t testing.TB, rel string) string { return string(Golden(t, rel)) }

// GoldenJSON decodes a JSON fixture into v.
func GoldenJSON(t testing.TB, rel string, v any) {
	t.Helper()
	if err := json.Unmarshal(Golden(t, rel), v); err != nil {
		t.Fatalf("decode golden %s: %v", rel, err)
	}
}

// GoldenNames lists the file names in a golden subdirectory with the given suffix.
func GoldenNames(t testing.TB, dir, suffix string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(GoldenDir(), filepath.FromSlash(dir)))
	if err != nil {
		t.Fatalf("list golden %s: %v", dir, err)
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && len(n) > len(suffix) && n[len(n)-len(suffix):] == suffix {
			names = append(names, n[:len(n)-len(suffix)])
		}
	}
	return names
}

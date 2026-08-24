package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanFindsTrackedSecretsAndIgnoresLocalConfiguration(t *testing.T) {
	root := t.TempDir()
	testToken := "glpat-" + strings.Repeat("a", 26)
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte(testToken), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env.local"), []byte(testToken), 0o600); err != nil {
		t.Fatal(err)
	}
	violations, err := scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 || violations[0] != "GitLab token: tracked.txt" {
		t.Fatalf("unexpected violations: %#v", violations)
	}
}

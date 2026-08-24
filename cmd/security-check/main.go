package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var patterns = map[string]*regexp.Regexp{
	"GitLab token":       regexp.MustCompile(`glpat-[A-Za-z0-9_-]{20,}`),
	"Mattermost webhook": regexp.MustCompile(`/hooks/[A-Za-z0-9_-]{12,}`),
}

func main() {
	root, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	violations, err := scan(root)
	if err != nil {
		fail(err)
	}
	if len(violations) == 0 {
		fmt.Println("Security check passed")
		return
	}
	for _, violation := range violations {
		fmt.Fprintln(os.Stderr, violation)
	}
	os.Exit(1)
}

func scan(root string) ([]string, error) {
	violations := []string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			base := entry.Name()
			if relative != "." && (base == ".git" || base == ".bundle" || base == ".local" || base == "vendor" || base == "data") {
				return filepath.SkipDir
			}
			return nil
		}
		base := entry.Name()
		if relative == "cmd/security-check/main.go" || base == ".env" || (strings.HasPrefix(base, ".env.") && base != ".env.example") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.IndexByte(content, 0) >= 0 {
			return nil
		}
		for name, pattern := range patterns {
			if pattern.Match(content) {
				violations = append(violations, name+": "+relative)
			}
		}
		return nil
	})
	sort.Strings(violations)
	return violations, err
}

func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }

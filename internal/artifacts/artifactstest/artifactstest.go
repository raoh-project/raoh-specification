// Package artifactstest builds specification trees for tests.
package artifactstest

import (
	"os"
	"path/filepath"
	"testing"
)

// Minimal holds the files every specification has, each with placeholder content.
var Minimal = map[string]string{
	"specification.json":               `{"version": "9.9.9-test"}`,
	"catalog/issues.json":              "{}",
	"catalog/operations.json":          "{}",
	"catalog/fixtures.json":            "{}",
	"catalog/messages/en.properties":   "",
	"catalog/messages/ja.properties":   "",
	"suite/retired.json":               "[]",
	"schema/issues.schema.json":        "{}",
	"schema/operations.schema.json":    "{}",
	"schema/fixtures.schema.json":      "{}",
	"schema/case.schema.json":          "{}",
	"schema/retired.schema.json":       "{}",
	"schema/runner-result.schema.json": "{}",
	"schema/conformance.schema.json":   "{}",
	"schema/report.schema.json":        "{}",
}

// Tree writes Minimal and extra, which overrides it, into a temporary directory.
func Tree(t *testing.T, extra map[string]string) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{}
	for k, v := range Minimal {
		files[k] = v
	}
	for k, v := range extra {
		files[k] = v
	}
	Write(t, root, files)
	return root
}

// Write writes files under root.
func Write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// Copy copies the real specification's machine-readable catalogues and schemas from the root at
// from into a tree, for tests that need the real vocabulary with a suite of their own.
func Copy(t *testing.T, from string, extra map[string]string) string {
	t.Helper()
	files := map[string]string{}
	for _, dir := range []string{"catalog", "schema"} {
		err := filepath.WalkDir(filepath.Join(from, dir), func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(from, p)
			text, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			files[filepath.ToSlash(rel)] = string(text)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for k, v := range extra {
		files[k] = v
	}
	return Tree(t, files)
}

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommands(t *testing.T) {
	for _, c := range []struct {
		args   []string
		status int
		out    string
	}{
		{[]string{"version"}, 0, "dev"},
		{[]string{"manifest", "../.."}, 0, "sha256:"},
		{[]string{"check-suite", "../.."}, 0, "cases"},
		{[]string{"features", "../.."}, 0, "operation.int32.min"},
		{[]string{"verify", "--spec", "../.."}, 2, ""},
		{[]string{"nonsense"}, 2, ""},
		{nil, 2, ""},
	} {
		var stdout, stderr bytes.Buffer
		status := run(c.args, &stdout, &stderr)
		if status != c.status || !strings.Contains(stdout.String(), c.out) {
			t.Errorf("%v: status %d, output %q, errors %q", c.args, status, stdout.String(), stderr.String())
		}
	}
}

// A run of verify is invalid on a suite that check-suite fails on: here one with no case that
// needs the features of bool.
func TestVerifyFailsWhereCheckSuiteFails(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"suite", "catalog", "schema", "spec"} {
		if err := os.CopyFS(filepath.Join(root, d), os.DirFS(filepath.Join("../..", d))); err != nil {
			t.Fatal(err)
		}
	}
	spec, err := os.ReadFile("../../specification.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "specification.json"), spec, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "suite", "core", "bool.json"), []byte("[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"check-suite", root},
		{"verify", "--spec", root, "--result", "none.json", "--conformance", "none.json"},
	} {
		var stdout, stderr bytes.Buffer
		status := run(args, &stdout, &stderr)
		if status == 0 || !strings.Contains(stderr.String(), "no case needs these features") {
			t.Errorf("%v: status %d, errors %q", args, status, stderr.String())
		}
	}
}

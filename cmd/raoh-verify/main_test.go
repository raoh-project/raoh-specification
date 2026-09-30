package main

import (
	"bytes"
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

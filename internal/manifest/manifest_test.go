package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raoh-project/raoh-specification/internal/artifacts/artifactstest"
)

func digest(t *testing.T, extra map[string]string) string {
	t.Helper()
	d, err := Digest(artifactstest.Tree(t, extra))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestLineEndingsDoNotChangeTheDigest(t *testing.T) {
	lf := digest(t, map[string]string{"suite/core/a.json": "[\n1\n]\n"})
	crlf := digest(t, map[string]string{"suite/core/a.json": "[\r\n1\r\n]\r\n"})
	if lf != crlf {
		t.Errorf("%s != %s", lf, crlf)
	}
	if !strings.HasPrefix(lf, "sha256:") || len(lf) != len("sha256:")+64 {
		t.Errorf("digest %s", lf)
	}
}

func TestContentPathsAndBoundariesChangeTheDigest(t *testing.T) {
	d := digest(t, map[string]string{"spec/a.md": "ab", "spec/b.md": "c"})
	for name, files := range map[string]map[string]string{
		"content":  {"spec/a.md": "ab", "spec/b.md": "d"},
		"path":     {"spec/a.md": "ab", "spec/c.md": "c"},
		"boundary": {"spec/a.md": "a", "spec/b.md": "bc"},
		"added":    {"spec/a.md": "ab", "spec/b.md": "c", "schema/x.schema.json": ""},
	} {
		if digest(t, files) == d {
			t.Errorf("changing the %s kept the digest", name)
		}
	}
}

func TestDotFilesAreNotArtifacts(t *testing.T) {
	with := map[string]string{"suite/.DS_Store": "x", "suite/core/.a.json.swp": "x", "spec/.hidden/x.md": "x"}
	if digest(t, nil) != digest(t, with) {
		t.Error("a dot file changed the digest")
	}
}

// A file that is not an artifact is an error, not something the digest quietly leaves out.
func TestStrayFilesAreErrors(t *testing.T) {
	for _, stray := range []string{"suite/suite.go", "catalog/x.json", "spec/notes.md~", "suite/core/A.json", "suite/other/a.json", "catalog/messages/fr.properties"} {
		if _, err := Digest(artifactstest.Tree(t, map[string]string{stray: "x"})); err == nil || !strings.Contains(err.Error(), stray) {
			t.Errorf("%s: %v", stray, err)
		}
	}
	root := artifactstest.Tree(t, nil)
	os.Remove(filepath.Join(root, "catalog", "fixtures.json"))
	if _, err := Digest(root); err == nil || !strings.Contains(err.Error(), "catalog/fixtures.json is missing") {
		t.Errorf("missing fixtures: %v", err)
	}
}

func TestFilesAreInByteOrder(t *testing.T) {
	files, err := Files(artifactstest.Tree(t, map[string]string{"suite/core/b.json": "", "suite/core/a_b.json": "", "spec/a.md": ""}))
	if err != nil {
		t.Fatal(err)
	}
	want := "catalog/fixtures.json,catalog/issues.json,catalog/messages/en.properties,catalog/messages/ja.properties,catalog/operations.json,schema/case.schema.json,schema/conformance.schema.json,schema/fixtures.schema.json,schema/issues.schema.json,schema/operations.schema.json,schema/report.schema.json,schema/retired.schema.json,schema/runner-result.schema.json,spec/a.md,specification.json,suite/core/a_b.json,suite/core/b.json,suite/retired.json"
	if strings.Join(files, ",") != want {
		t.Errorf("%v", files)
	}
}

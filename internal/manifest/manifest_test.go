package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func digest(t *testing.T, files map[string]string) string {
	t.Helper()
	d, err := Digest(tree(t, files))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestLineEndingsDoNotChangeTheDigest(t *testing.T) {
	lf := digest(t, map[string]string{"specification.json": "{}\n", "suite/core/a.json": "[\n1\n]\n"})
	crlf := digest(t, map[string]string{"specification.json": "{}\r\n", "suite/core/a.json": "[\r\n1\r\n]\r\n"})
	if lf != crlf {
		t.Errorf("%s != %s", lf, crlf)
	}
	if !strings.HasPrefix(lf, "sha256:") || len(lf) != len("sha256:")+64 {
		t.Errorf("digest %s", lf)
	}
}

func TestContentPathsAndBoundariesChangeTheDigest(t *testing.T) {
	base := map[string]string{"specification.json": "{}", "spec/a.md": "ab", "spec/b.md": "c"}
	d := digest(t, base)
	for name, files := range map[string]map[string]string{
		"content":  {"specification.json": "{}", "spec/a.md": "ab", "spec/b.md": "d"},
		"path":     {"specification.json": "{}", "spec/a.md": "ab", "spec/c.md": "c"},
		"boundary": {"specification.json": "{}", "spec/a.md": "a", "spec/b.md": "bc"},
		"added":    {"specification.json": "{}", "spec/a.md": "ab", "spec/b.md": "c", "catalog/x.json": ""},
	} {
		if digest(t, files) == d {
			t.Errorf("changing the %s kept the digest", name)
		}
	}
}

func TestFilesOutsideTheManifestAreIgnored(t *testing.T) {
	base := map[string]string{"specification.json": "{}", "suite/core/a.json": "[]"}
	with := map[string]string{"specification.json": "{}", "suite/core/a.json": "[]", "README.md": "x", "value/x.go": "package value"}
	if digest(t, base) != digest(t, with) {
		t.Error("a file outside the manifest changed the digest")
	}
}

func TestOnlySpecificationFilesAreInTheManifest(t *testing.T) {
	base := map[string]string{"specification.json": "{}", "suite/core/a.json": "[]", "catalog/messages/en.properties": "a=b"}
	with := map[string]string{"specification.json": "{}", "suite/core/a.json": "[]", "catalog/messages/en.properties": "a=b",
		"suite/.DS_Store": "x", "catalog/catalog.go": "package catalog", "spec/notes.md~": "x", "suite/core/.a.json.swp": "x"}
	if digest(t, base) != digest(t, with) {
		t.Error("a file that is not part of the specification changed the digest")
	}
}

func TestFilesAreInByteOrder(t *testing.T) {
	root := tree(t, map[string]string{"specification.json": "", "suite/core/a.json": "", "suite/core/B.json": "", "catalog/z.json": "", "spec/a.md": ""})
	files, err := Files(root)
	if err != nil {
		t.Fatal(err)
	}
	want := "catalog/z.json,spec/a.md,specification.json,suite/core/B.json,suite/core/a.json"
	if strings.Join(files, ",") != want {
		t.Errorf("%v", files)
	}
}

package specification_test

import (
	"io/fs"
	"os"
	"path"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

var (
	heading = regexp.MustCompile("^#{1,6}\\s+(.*?)\\s*$")
	link    = regexp.MustCompile(`\]\(([^)\s]+)\)`)
)

// slug is the anchor GitHub gives a heading: lower case, punctuation other than hyphens dropped,
// spaces made hyphens.
func slug(h string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.NewReplacer("`", "", "*", "").Replace(h)) {
		switch {
		case r == ' ' || r == '-':
			b.WriteByte('-')
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
			b.WriteRune(r)
		}
	}
	return b.String()
}

// A link between the documents has to lead somewhere: to a file that exists and, when it names a
// heading, to a heading the file has. The documents state the same facts in several places and
// point from one to another, so a heading renamed or a file moved shows here and not to a reader.
func TestLinksBetweenDocumentsResolve(t *testing.T) {
	root := os.DirFS(".")
	var docs []string
	err := fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (p == ".git" || p == ".github") {
			return fs.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(p, ".md") {
			docs = append(docs, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	anchors := map[string]map[string]bool{}
	text := map[string]string{}
	for _, doc := range docs {
		b, err := fs.ReadFile(root, doc)
		if err != nil {
			t.Fatal(err)
		}
		text[doc] = string(b)
		anchors[doc] = map[string]bool{}
		for _, line := range strings.Split(string(b), "\n") {
			if m := heading.FindStringSubmatch(line); m != nil {
				anchors[doc][slug(m[1])] = true
			}
		}
	}
	for _, doc := range docs {
		for n, line := range strings.Split(text[doc], "\n") {
			for _, m := range link.FindAllStringSubmatch(line, -1) {
				target, fragment, _ := strings.Cut(m[1], "#")
				if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
					continue
				}
				file := doc
				if target != "" {
					file = path.Join(path.Dir(doc), target)
				}
				if _, err := fs.Stat(root, file); err != nil {
					t.Errorf("%s:%d: %s: no such file", doc, n+1, m[1])
					continue
				}
				if fragment != "" && strings.HasSuffix(file, ".md") && !anchors[file][fragment] {
					t.Errorf("%s:%d: %s: %s has no heading with that anchor", doc, n+1, m[1], file)
				}
			}
		}
	}
}

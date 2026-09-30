// Package artifacts defines the normative artifact set: every file the specification consists
// of. The manifest digest covers exactly this set, and every loader reads its files from it, so
// that what a digest vouches for and what the verifier reads cannot drift apart.
package artifacts

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Kind is what an artifact is.
type Kind int

// The kinds of artifact.
const (
	// Version is specification.json.
	Version Kind = iota
	// Prose is normative text: spec/*.md and suite/*.md. The verifier does not parse it.
	Prose
	// IssueCatalog is catalog/issues.json.
	IssueCatalog
	// Operations is catalog/operations.json.
	Operations
	// Fixtures is catalog/fixtures.json.
	Fixtures
	// Messages is catalog/messages/<locale>.properties.
	Messages
	// Schema is schema/<name>.schema.json.
	Schema
	// CaseFile is suite/<profile>/<name>.json.
	CaseFile
	// RetiredIDs is suite/retired.json.
	RetiredIDs
)

// Artifact is one file of the specification.
type Artifact struct {
	// Path is relative to the root and /-separated.
	Path string
	Kind Kind
	// Profile is the profile of a case file, and Locale the locale of a message catalogue.
	Profile, Locale string
}

// Dirs are the directories that hold artifacts, besides the root's specification.json.
var Dirs = []string{"spec", "catalog", "schema", "suite"}

// Locales are the locales of the message catalogues.
var Locales = []string{"en", "ja"}

// CaseProfiles are the profiles whose cases are files under suite/.
var CaseProfiles = []string{"core", "encode"}

var (
	proseRe    = regexp.MustCompile(`^(spec/[a-z0-9-]+|suite/[A-Z]+)\.md$`)
	messagesRe = regexp.MustCompile(`^catalog/messages/([a-z]+)\.properties$`)
	schemaRe   = regexp.MustCompile(`^schema/[a-z-]+\.schema\.json$`)
	caseRe     = regexp.MustCompile(`^suite/([a-z]+)/[a-z0-9_]+\.json$`)
	exact      = map[string]Kind{
		"specification.json":      Version,
		"catalog/issues.json":     IssueCatalog,
		"catalog/operations.json": Operations,
		"catalog/fixtures.json":   Fixtures,
		"suite/retired.json":      RetiredIDs,
	}
	required = []string{"specification.json", "catalog/issues.json", "catalog/operations.json", "catalog/fixtures.json", "suite/retired.json",
		"schema/issues.schema.json", "schema/operations.schema.json", "schema/fixtures.schema.json", "schema/case.schema.json",
		"schema/retired.schema.json", "schema/runner-result.schema.json", "schema/conformance.schema.json", "schema/report.schema.json"}
)

// Classify says what the file at a relative path is, or that it is not part of the
// specification.
func Classify(rel string) (Artifact, error) {
	a := Artifact{Path: rel}
	if k, ok := exact[rel]; ok {
		a.Kind = k
		return a, nil
	}
	if proseRe.MatchString(rel) {
		a.Kind = Prose
		return a, nil
	}
	if m := messagesRe.FindStringSubmatch(rel); m != nil && contains(Locales, m[1]) {
		a.Kind, a.Locale = Messages, m[1]
		return a, nil
	}
	if schemaRe.MatchString(rel) {
		a.Kind = Schema
		return a, nil
	}
	if m := caseRe.FindStringSubmatch(rel); m != nil && contains(CaseProfiles, m[1]) {
		a.Kind, a.Profile = CaseFile, m[1]
		return a, nil
	}
	return a, fmt.Errorf("%s is not part of the specification: no kind of artifact has that name", rel)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// List gives every artifact under root, in byte order of their paths. A file in one of Dirs that
// is not an artifact is an error, not something to skip; only names that start with a dot (.git,
// .DS_Store, an editor's swap file) are passed over.
func List(root string) ([]Artifact, error) {
	var list []Artifact
	var problems []string
	add := func(rel string) {
		a, err := Classify(rel)
		if err != nil {
			problems = append(problems, err.Error())
			return
		}
		list = append(list, a)
	}
	if _, err := os.Stat(filepath.Join(root, "specification.json")); err == nil {
		add("specification.json")
	}
	for _, dir := range Dirs {
		err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if strings.HasPrefix(d.Name(), ".") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if !d.Type().IsRegular() {
				problems = append(problems, fmt.Sprintf("%s is not a regular file", p))
				return nil
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			add(filepath.ToSlash(rel))
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	for _, r := range required {
		if !containsPath(list, r) {
			problems = append(problems, fmt.Sprintf("%s is missing", r))
		}
	}
	for _, l := range Locales {
		if !containsPath(list, path.Join("catalog/messages", l+".properties")) {
			problems = append(problems, fmt.Sprintf("catalog/messages/%s.properties is missing", l))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return nil, fmt.Errorf("the specification's files are not as they should be:\n  %s", strings.Join(problems, "\n  "))
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Path < list[j].Path })
	return list, nil
}

func containsPath(list []Artifact, p string) bool {
	for _, a := range list {
		if a.Path == p {
			return true
		}
	}
	return false
}

// OfKind gives the artifacts of a kind, in order.
func OfKind(list []Artifact, k Kind) []Artifact {
	var out []Artifact
	for _, a := range list {
		if a.Kind == k {
			out = append(out, a)
		}
	}
	return out
}

// Command raoh-verify checks the specification's suite and verifies an implementation's runner
// result against it. See spec/conformance.md.
//
// Usage:
//
//	raoh-verify check-suite <spec-root>
//	raoh-verify manifest <spec-root>
//	raoh-verify features <spec-root>
//	raoh-verify check-ids <base-spec-root> <spec-root>
//	raoh-verify verify --spec <spec-root> --result <runner-result.json> --conformance <conformance.json> [-o <report.json>]
//	raoh-verify version
//
// verify exits with status 0 when no profile is non-conformant, 1 when one is, and 2 when the input
// cannot be trusted. check-suite exits with status 1 when the suite has problems.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/raoh-project/raoh-specification/internal/manifest"
	"github.com/raoh-project/raoh-specification/internal/suite"
	"github.com/raoh-project/raoh-specification/internal/verify"
)

// version is set at release with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: raoh-verify check-suite|manifest|features|check-ids|verify|version ...")
		return 2
	}
	switch args[0] {
	case "check-suite":
		root, ok := oneRoot(args[1:], stderr)
		if !ok {
			return 2
		}
		s, err := verify.Load(root)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if problem := unlistedOrUnpinned(s); problem != "" {
			fmt.Fprint(stderr, problem)
			return 1
		}
		fmt.Fprintf(stdout, "specification %s: %d cases, %d features, manifest %s\n", s.Version, len(s.Suite.Cases), len(s.Features), s.Digest)
		return 0
	case "manifest":
		root, ok := oneRoot(args[1:], stderr)
		if !ok {
			return 2
		}
		d, err := manifest.Digest(root)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		fmt.Fprintln(stdout, d)
		return 0
	case "features":
		root, ok := oneRoot(args[1:], stderr)
		if !ok {
			return 2
		}
		s, err := verify.Load(root)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		for _, f := range s.Checker.Registry().Features() {
			fmt.Fprintln(stdout, f)
		}
		return 0
	case "check-ids":
		if len(args) != 3 {
			fmt.Fprintln(stderr, "check-ids takes the root of the base revision and the root of the changed one")
			return 2
		}
		base, err := suite.LoadIDs(args[1])
		if err != nil {
			fmt.Fprintln(stderr, "base:", err)
			return 2
		}
		head, err := verify.Load(args[2])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := suite.CheckIDs(base, head.Suite); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "%d case IDs kept, %d retired\n", len(base.Cases), len(head.Suite.Retired))
		return 0
	case "verify":
		return runVerify(args[1:], stdout, stderr)
	case "version":
		fmt.Fprintln(stdout, version)
		return 0
	}
	fmt.Fprintf(stderr, "unknown command %q\n", args[0])
	return 2
}

// unlistedOrUnpinned says what the suite leaves unpinned that the catalogues list or take, or is
// empty when it leaves nothing. check-suite fails on it, and so does a run of verify.
func unlistedOrUnpinned(s *verify.Spec) string {
	if uncovered := s.Uncovered(); len(uncovered) > 0 {
		return fmt.Sprintf("no case needs these features, so the registry cannot list them:\n  %s\n", strings.Join(uncovered, "\n  "))
	}
	if ungiven := s.UngivenMessages(); len(ungiven) > 0 {
		return fmt.Sprintf("no case gives these issues the message its form takes:\n  %s\n", strings.Join(ungiven, "\n  "))
	}
	if unpinned := s.UnpinnedOptional(); len(unpinned) > 0 {
		return fmt.Sprintf("no case leaves out these optional metadata entries, so optional_meta cannot list them:\n  %s\n", strings.Join(unpinned, "\n  "))
	}
	if unpinned := s.UnpinnedCandidatePaths(); len(unpinned) > 0 {
		return fmt.Sprintf("no case lists a candidate's issue below the root, under an issue below it, for these issues, so the reading of a candidate's path is not pinned:\n  %s\n", strings.Join(unpinned, "\n  "))
	}
	return ""
}

func oneRoot(args []string, stderr io.Writer) (string, bool) {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "expected the root of raoh-specification")
		return "", false
	}
	return args[0], true
}

func runVerify(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	specRoot := fs.String("spec", "", "the root of raoh-specification at the revision the runner read")
	resultPath := fs.String("result", "", "the runner result")
	declPath := fs.String("conformance", "", "the implementation's conformance.json")
	out := fs.String("o", "", "where to write the report (standard output if not given)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *specRoot == "" || *resultPath == "" || *declPath == "" || fs.NArg() > 0 {
		fmt.Fprintln(stderr, "verify needs --spec, --result and --conformance")
		return 2
	}
	s, err := verify.Load(*specRoot)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if problem := unlistedOrUnpinned(s); problem != "" {
		fmt.Fprint(stderr, problem)
		return 2
	}
	result, err := os.ReadFile(*resultPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	decl, err := os.ReadFile(*declPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	// Every error Verify gives, an *verify.InvalidError or not, means nothing was compared.
	report, err := verify.Verify(s, result, decl, version)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	text, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	text = append(text, '\n')
	if *out == "" {
		stdout.Write(text)
	} else if err := os.WriteFile(*out, text, 0o644); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	fmt.Fprintln(stderr, summary(report))
	for _, c := range report.Cases {
		if c.Outcome == verify.Failed {
			fmt.Fprintf(stderr, "  failed %s %s: %s\n", c.Profile, c.ID, c.Detail)
		}
	}
	if !report.Conformant() {
		return 1
	}
	return 0
}

// summary states the result as spec/conformance.md says an implementation states it.
func summary(r *verify.Report) string {
	var parts []string
	for _, name := range verify.Profiles {
		p, ok := r.Profiles[name]
		if !ok {
			continue
		}
		s := name + ": " + strings.ReplaceAll(p.Status, "_", " ")
		var counts []string
		if p.Failed > 0 {
			counts = append(counts, fmt.Sprintf("%d failed", p.Failed))
		}
		if p.Divergent > 0 {
			counts = append(counts, fmt.Sprintf("%d divergent", p.Divergent))
		}
		if p.Unsupported > 0 {
			counts = append(counts, fmt.Sprintf("%d unsupported", p.Unsupported))
		}
		if len(counts) > 0 {
			s += " (" + strings.Join(counts, ", ") + ")"
		}
		parts = append(parts, s)
	}
	return fmt.Sprintf("%s %s against Raoh Specification %s — %s", r.Implementation.Name, r.Implementation.Version, r.Specification.Version, strings.Join(parts, "; "))
}

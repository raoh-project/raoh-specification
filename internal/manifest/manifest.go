// Package manifest computes the digest that ties a runner's result to the exact revision of the
// specification it ran. See spec/conformance.md.
package manifest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"

	"github.com/raoh-project/raoh-specification/internal/artifacts"
)

// Files lists the files the digest covers: the normative artifact set, in byte order of paths.
func Files(root string) ([]string, error) {
	list, err := artifacts.List(root)
	if err != nil {
		return nil, err
	}
	files := make([]string, len(list))
	for i, a := range list {
		files[i] = a.Path
	}
	return files, nil
}

// Digest computes the manifest digest of the specification under root.
func Digest(root string) (string, error) {
	files, err := Files(root)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, rel := range files {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return "", err
		}
		content = bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
		h.Write([]byte(strconv.Itoa(len(rel)) + "\n" + rel + "\n" + strconv.Itoa(len(content)) + "\n"))
		h.Write(content)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

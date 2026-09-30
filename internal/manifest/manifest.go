// Package manifest computes the digest that ties a runner's result to the exact revision of the
// specification it ran. See spec/conformance.md.
package manifest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Dirs are the directories whose files the digest covers, besides specification.json.
var Dirs = []string{"spec", "catalog", "schema", "suite"}

// Extensions are the kinds of file the digest covers. Anything else under Dirs (an editor's
// backup, a .DS_Store) is not part of the specification, so that the same revision gives the same
// digest on every machine.
var Extensions = []string{".md", ".json", ".properties"}

// Files lists the files the digest covers, as relative /-separated paths in byte order.
func Files(root string) ([]string, error) {
	files := []string{"specification.json"}
	for _, dir := range Dirs {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") || !slices.Contains(Extensions, filepath.Ext(d.Name())) {
				return nil
			}
			if !d.Type().IsRegular() {
				return fmt.Errorf("%s is not a regular file", path)
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(rel))
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	sort.Strings(files)
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

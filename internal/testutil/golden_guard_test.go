package testutil

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var goldenNamesCall = regexp.MustCompile(`GoldenNames\([^,]+,\s*"([^"]+)",\s*"([^"]+)"\)`)

var derivedName = regexp.MustCompile(`"([^"/]+)/"\s*\+\s*\w+\s*\+\s*"(\.[^"]+)"`)

// dirAllowlist lists directories read as a whole (walked or copied by a test);
// every file below them counts as read.
var dirAllowlist = map[string]string{
	"feeds/posts": "internal/feeds and internal/cli golden tests build feeds from the whole posts dir",
	"feeds/bad":   "internal/cli golden test (feeds_build_bad_posts) builds from the whole dir; expected messages in feeds/bad_posts.json",
	"opml/in":     "internal/feeds OPML tests read every input file of the directory",
	"cli":         "internal/cli TestCLIGolden reads <case>.stdout/.stderr/.exitcode/<file> by case name",
}

// dirReadAllowlist lists golden files that no test names directly and that are
// not found through a suffix listing (GoldenNames) either; each entry says
// which test reads it. Keep this list short: prefer a test that names the file.
var dirReadAllowlist = map[string]string{
	// Known orphans, found when the guard was added: no test reads them. Either
	// add a test that does (or delete the file) and remove the entry.
	"crypto/load_public_key_b64_der.json": "ORPHAN: not read by any test",
	"crypto/pem_errors.json":              "ORPHAN: not read by any test",
	"opml/warnings.json":                  "ORPHAN: not read by any test",
	"qr/image.json":                       "ORPHAN: not read by any test",
}

// TestGoldenFilesAreReferenced fails when a file under testdata/golden is not
// read by any test, so orphaned fixtures are noticed. A file counts as read when
// a *_test.go under internal/ contains, as text:
//   - its path relative to testdata/golden, or its base name;
//   - a GoldenNames(t, "dir", ".suffix") call matching its directory and
//     suffix (how the per-case files in vcards/ are read);
//   - a "dir/"+name+".suffix" expression (case names come from a listing);
//   - it is under a directory in dirAllowlist, or is in dirReadAllowlist,
//     each with a comment saying which test reads it.
func TestGoldenFilesAreReferenced(t *testing.T) {
	root := GoldenDir()
	internal := filepath.Join(root, "..", "..", "internal")
	var src strings.Builder
	err := filepath.WalkDir(internal, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// This file lists the allowlist and would match everything it mentions.
		if !d.IsDir() && strings.HasSuffix(p, "_test.go") && filepath.Base(p) != "golden_guard_test.go" {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			src.Write(b)
			src.WriteByte('\n')
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	text := src.String()

	// GoldenNames(t, "dir", ".suffix") calls: every matching file is read.
	listings := goldenNamesCall.FindAllStringSubmatch(text, -1)
	// "dir/"+name+".suffix" reads: the case names come from a listing.
	listings = append(listings, derivedName.FindAllStringSubmatch(text, -1)...)

	var orphans []string
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		base := d.Name()
		if _, ok := dirReadAllowlist[rel]; ok {
			return nil
		}
		for dir := range dirAllowlist {
			if strings.HasPrefix(rel, dir+"/") {
				return nil
			}
		}
		if strings.Contains(text, `"`+rel+`"`) || strings.Contains(text, `"`+base+`"`) {
			return nil
		}
		listed := false
		for _, m := range listings {
			if m[1] == filepath.ToSlash(filepath.Dir(rel)) && strings.HasSuffix(base, m[2]) {
				listed = true
			}
		}
		if listed {
			return nil
		}
		orphans = append(orphans, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(orphans)
	for _, o := range orphans {
		t.Errorf("golden fixture %s is not read by any test (remove it or reference it; see testdata/README.md)", o)
	}
	for rel := range mergeKeys(dirReadAllowlist, dirAllowlist) {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Errorf("allowlist entry %s does not exist", rel)
		}
	}
}

func mergeKeys(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

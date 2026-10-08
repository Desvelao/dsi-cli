package core

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var allowedVCardExtensions = []string{".vcf", ".vcard"}

// FileIsVCardName reports whether a file name ends with a vCard extension
// (a plain suffix check).
func FileIsVCardName(name string) bool {
	lower := strings.ToLower(name)
	for _, ext := range allowedVCardExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// FileIsVCardPath reports whether a path has a vCard extension (a
// path-suffix check: a name like ".vcf" has no suffix).
func FileIsVCardPath(path string) bool {
	name := filepath.Base(path)
	i := strings.LastIndex(name, ".")
	if i <= 0 || i >= len(name)-1 {
		return false
	}
	suffix := strings.ToLower(name[i:])
	for _, ext := range allowedVCardExtensions {
		if suffix == ext {
			return true
		}
	}
	return false
}

// LocalFilesFromInputs collects the matching files of the given paths
// (directories are walked recursively in lexical order; symlinks inside
// walked directories are skipped, both to files and directories, so reads
// cannot escape the input tree). Top-level inputs given explicitly are
// resolved with os.Stat, so a symlink named directly is followed; problems
// with inputs are appended to warnings.
func LocalFilesFromInputs(inputs []string, filter func(string) bool, warnings *[]string) []string {
	var files []string
	warn := func(format string, args ...any) {
		if warnings != nil {
			*warnings = append(*warnings, fmt.Sprintf(format, args...))
		}
	}
	for _, in := range inputs {
		info, err := os.Stat(in)
		if err != nil {
			warn("Input path does not exist: %s", in)
			continue
		}
		switch {
		case info.Mode().IsRegular():
			if filter(in) {
				files = append(files, in)
			} else {
				warn("Skipping file without a vCard extension: %s", in)
			}
		case info.IsDir():
			_ = filepath.WalkDir(in, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					warn("Cannot read %s: %v", p, err)
					return nil
				}
				if d.Type()&fs.ModeSymlink != 0 {
					return nil
				}
				if !d.IsDir() && filter(p) {
					files = append(files, p)
				}
				return nil
			})
		default:
			warn("Input path is not a file or directory: %s", in)
		}
	}
	return files
}

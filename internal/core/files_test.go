package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileIsVCardNameTable(t *testing.T) {
	for name, want := range map[string]bool{
		"a.vcf": true, "a.VCF": true, "a.vcard": true, "a.VCard": true,
		"a.vcf.txt": false, "a.txt": false, "vcf": false, "": false, "a.vcfx": false,
	} {
		if got := FileIsVCardName(name); got != want {
			t.Errorf("FileIsVCardName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestFileIsVCardPathTable(t *testing.T) {
	for path, want := range map[string]bool{
		"a.vcf": true, "dir/a.VCARD": true, "dir.vcf/a": false, "dir/.vcf": false,
		"dir/a.": false, "dir/a.txt": false, "a.b.vcf": true, "": false, "dir/a.vcf.bak": false,
	} {
		if got := FileIsVCardPath(path); got != want {
			t.Errorf("FileIsVCardPath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestLocalFilesFromInputsBehaviour(t *testing.T) {
	dir := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(dir, "b", "deep"), 0o755))
	must(os.WriteFile(filepath.Join(dir, "z.vcf"), nil, 0o644))
	must(os.WriteFile(filepath.Join(dir, "b", "deep", "a.vcf"), nil, 0o644))
	must(os.WriteFile(filepath.Join(dir, "b", "m.vcard"), nil, 0o644))
	must(os.WriteFile(filepath.Join(dir, "notes.txt"), nil, 0o644))

	t.Run("directory walked in lexical order", func(t *testing.T) {
		got := LocalFilesFromInputs([]string{dir}, FileIsVCardPath, nil)
		want := []string{filepath.Join(dir, "b", "deep", "a.vcf"), filepath.Join(dir, "b", "m.vcard"), filepath.Join(dir, "z.vcf")}
		if len(got) != len(want) {
			t.Fatalf("got %v want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("got %v want %v", got, want)
			}
		}
	})
	t.Run("explicit file is filtered", func(t *testing.T) {
		var w []string
		got := LocalFilesFromInputs([]string{filepath.Join(dir, "notes.txt"), filepath.Join(dir, "z.vcf")}, FileIsVCardPath, &w)
		if len(got) != 1 || got[0] != filepath.Join(dir, "z.vcf") || len(w) != 1 ||
			!strings.Contains(w[0], "notes.txt") || !strings.Contains(w[0], "Skipping") {
			t.Errorf("got %v warnings %v", got, w)
		}
	})
	t.Run("unreadable subdirectory warns and is skipped", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("permissions are not enforced for root")
		}
		root := t.TempDir()
		locked := filepath.Join(root, "locked")
		must(os.MkdirAll(locked, 0o755))
		must(os.WriteFile(filepath.Join(root, "ok.vcf"), nil, 0o644))
		must(os.Chmod(locked, 0o000))
		t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
		var w []string
		got := LocalFilesFromInputs([]string{root}, FileIsVCardPath, &w)
		if len(got) != 1 || got[0] != filepath.Join(root, "ok.vcf") {
			t.Errorf("got %v", got)
		}
		if len(w) != 1 || !strings.Contains(w[0], locked) {
			t.Errorf("warnings %v", w)
		}
	})
	t.Run("missing input with nil warnings does not panic", func(t *testing.T) {
		if got := LocalFilesFromInputs([]string{filepath.Join(dir, "nope")}, FileIsVCardPath, nil); len(got) != 0 {
			t.Errorf("got %v", got)
		}
	})
	t.Run("no inputs", func(t *testing.T) {
		if got := LocalFilesFromInputs(nil, FileIsVCardPath, nil); len(got) != 0 {
			t.Errorf("got %v", got)
		}
	})
}

func TestLocalFilesFromInputsSkipsSymlinksInWalkedDirs(t *testing.T) {
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.vcf")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "real.vcf")
	if err := os.WriteFile(real, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "link.vcf")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "linkdir")); err != nil {
		t.Fatal(err)
	}
	got := LocalFilesFromInputs([]string{dir}, FileIsVCardPath, nil)
	if len(got) != 1 || got[0] != real {
		t.Errorf("walked files = %v, want only %s", got, real)
	}
	// An explicitly named top-level symlink is still followed.
	got = LocalFilesFromInputs([]string{filepath.Join(dir, "link.vcf")}, FileIsVCardPath, nil)
	if len(got) != 1 {
		t.Errorf("top-level symlink = %v, want 1 file", got)
	}
}

func TestNewVCardFromPathSizeCap(t *testing.T) {
	dir := t.TempDir()
	prefix := "BEGIN:VCARD\nVERSION:4.0\nFN:A\nNOTE:"
	mk := func(name string, size int) string {
		p := filepath.Join(dir, name)
		body := prefix + strings.Repeat("a", size-len(prefix))
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if _, err := NewVCardFromPath(mk("ok.vcf", DefaultMaxBytes)); err != nil {
		t.Errorf("file at the cap rejected: %v", err)
	}
	big := mk("big.vcf", DefaultMaxBytes+1)
	_, err := NewVCardFromPath(big)
	if err == nil || !strings.Contains(err.Error(), big) || !strings.Contains(err.Error(), "1000000") {
		t.Errorf("want size error naming file and limit, got %v", err)
	}
}

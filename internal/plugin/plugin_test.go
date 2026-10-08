package plugin

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func script(t *testing.T, dir, name string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestValidName(t *testing.T) {
	for name, want := range map[string]bool{
		"hello": true, "publish-s3": true, "a.b_c": true, "9x": true,
		"": false, "-x": false, ".x": false, "../x": false, "a/b": false, `a\b`: false, "a b": false, "a\x00": false,
	} {
		if ValidName(name) != want {
			t.Errorf("ValidName(%q) != %v", name, want)
		}
	}
}

func TestFindAndListPreferEarlierDirectories(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts")
	}
	first, second := t.TempDir(), t.TempDir()
	want := script(t, first, "dsi-hello", 0o755)
	script(t, second, "dsi-hello", 0o755)
	other := script(t, second, "dsi-other", 0o755)
	script(t, second, "dsi-noexec", 0o644)
	script(t, second, "not-a-plugin", 0o755)
	os.Mkdir(filepath.Join(second, "dsi-dir"), 0o755)

	f := Finder{Dirs: []string{first, second, filepath.Join(t.TempDir(), "missing")}}
	if got, ok := f.Find("hello"); !ok || got != want {
		t.Errorf("Find(hello) = %q, %v", got, ok)
	}
	if got, ok := f.Find("other"); !ok || got != other {
		t.Errorf("Find(other) = %q, %v", got, ok)
	}
	for _, name := range []string{"noexec", "dir", "missing", "../dsi-hello", ""} {
		if p, ok := f.Find(name); ok {
			t.Errorf("Find(%q) must fail, got %q", name, p)
		}
	}
	list := f.List()
	if len(list) != 2 || list[0].Name != "hello" || list[0].Path != want || list[1].Name != "other" {
		t.Errorf("List() = %+v", list)
	}
}

func TestPluginDirResolution(t *testing.T) {
	t.Setenv("DSI_PLUGIN_DIR", "/custom")
	if PluginDir() != "/custom" {
		t.Errorf("DSI_PLUGIN_DIR: %q", PluginDir())
	}
	t.Setenv("DSI_PLUGIN_DIR", "")
	t.Setenv("XDG_DATA_HOME", "/xdg")
	if PluginDir() != filepath.Join("/xdg", "dsi", "plugins") {
		t.Errorf("XDG_DATA_HOME: %q", PluginDir())
	}
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "/home/u")
	if got := PluginDir(); got != filepath.Join("/home/u", ".local", "share", "dsi", "plugins") {
		t.Errorf("default: %q", got)
	}
}

func TestDefaultFinderOrderAndEmptyPathEntries(t *testing.T) {
	t.Setenv("DSI_PLUGIN_DIR", "/plugins")
	t.Setenv("PATH", "/a::/b")
	got := DefaultFinder().Dirs
	want := []string{"/plugins", "/a", "/b"}
	if len(got) != len(want) {
		t.Fatalf("dirs: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("dirs: %v", got)
		}
	}
}

func TestPathExtHelpers(t *testing.T) {
	if got := executableNames("linux", ".EXE;.BAT", "x"); len(got) != 1 || got[0] != "dsi-x" {
		t.Errorf("linux names = %v", got)
	}
	got := executableNames("windows", ".COM;.exe; .Bat ;;", "x")
	want := []string{"dsi-x.COM", "dsi-x.exe", "dsi-x.Bat"}
	if len(got) != len(want) {
		t.Fatalf("names = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("names = %v, want %v", got, want)
		}
	}
	if got := executableNames("windows", "", "x"); len(got) != 4 || got[3] != "dsi-x.CMD" {
		t.Errorf("default names = %v", got)
	}
	for in, out := range map[string]string{
		"dsi-x.cmd": "dsi-x", "dsi-x.BAT": "dsi-x", "dsi-x.txt": "dsi-x.txt", "dsi-x": "dsi-x",
	} {
		if got := trimExt("windows", ".CMD;.BAT;.EXE", in); got != out {
			t.Errorf("trimExt(%q) = %q, want %q", in, got, out)
		}
	}
	if got := trimExt("linux", ".CMD", "dsi-x.cmd"); got != "dsi-x.cmd" {
		t.Errorf("trimExt on linux = %q", got)
	}
}

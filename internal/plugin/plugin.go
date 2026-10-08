// Package plugin finds and runs external dsi plugins: executables named
// "dsi-<name>" that are run as "dsi <name> ...", like git or gh extensions.
package plugin

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

// Prefix is the file name prefix of plugin executables.
const Prefix = "dsi-"

var validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Plugin is an installed plugin.
type Plugin struct {
	Name string // "hello" for dsi-hello
	Path string
}

// Finder looks for plugins in an ordered list of directories.
type Finder struct{ Dirs []string }

// PluginDir is the directory plugins are installed into: $DSI_PLUGIN_DIR, or
// $XDG_DATA_HOME/dsi/plugins (default ~/.local/share/dsi/plugins).
func PluginDir() string {
	if d := os.Getenv("DSI_PLUGIN_DIR"); d != "" {
		return d
	}
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "dsi", "plugins")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "share", "dsi", "plugins")
	}
	return ""
}

// DefaultFinder searches the plugin directory first, then $PATH. Empty PATH
// entries are ignored (they would mean the current directory).
func DefaultFinder() Finder {
	var dirs []string
	if d := PluginDir(); d != "" {
		dirs = append(dirs, d)
	}
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		if d != "" {
			dirs = append(dirs, d)
		}
	}
	return Finder{Dirs: dirs}
}

// ValidName reports whether name can be a plugin name: no path separators, no
// leading dash or dot.
func ValidName(name string) bool { return validName.MatchString(name) }

// defaultPathExt is what Windows uses when PATHEXT is unset.
const defaultPathExt = ".COM;.EXE;.BAT;.CMD"

// pathExts returns the executable extensions for goos: none outside Windows,
// otherwise the entries of pathext (the PATHEXT value; defaults when empty).
func pathExts(goos, pathext string) []string {
	if goos != "windows" {
		return nil
	}
	if strings.TrimSpace(pathext) == "" {
		pathext = defaultPathExt
	}
	var exts []string
	for _, e := range strings.Split(pathext, ";") {
		if e = strings.TrimSpace(e); e != "" {
			exts = append(exts, e)
		}
	}
	return exts
}

// executableNames returns the file names a plugin can have, in lookup order.
func executableNames(goos, pathext, name string) []string {
	exts := pathExts(goos, pathext)
	if exts == nil {
		return []string{Prefix + name}
	}
	names := make([]string, 0, len(exts))
	for _, e := range exts {
		names = append(names, Prefix+name+e)
	}
	return names
}

// trimExt removes a PATHEXT extension (case-insensitively) from file.
func trimExt(goos, pathext, file string) string {
	for _, e := range pathExts(goos, pathext) {
		if len(file) > len(e) && strings.EqualFold(file[len(file)-len(e):], e) {
			return file[:len(file)-len(e)]
		}
	}
	return file
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	return runtime.GOOS == "windows" || info.Mode().Perm()&0o111 != 0
}

// Find returns the path of the plugin with the given name.
func (f Finder) Find(name string) (string, bool) {
	if !ValidName(name) {
		return "", false
	}
	for _, dir := range f.Dirs {
		for _, file := range executableNames(runtime.GOOS, os.Getenv("PATHEXT"), name) {
			path := filepath.Join(dir, file)
			if isExecutable(path) {
				return path, true
			}
		}
	}
	return "", false
}

// List returns every installed plugin, sorted by name; when a name appears in
// several directories the first one wins.
func (f Finder) List() []Plugin {
	seen := map[string]bool{}
	var plugins []Plugin
	for _, dir := range f.Dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			file := trimExt(runtime.GOOS, os.Getenv("PATHEXT"), e.Name())
			if !strings.HasPrefix(file, Prefix) {
				continue
			}
			name := strings.TrimPrefix(file, Prefix)
			if seen[name] || !ValidName(name) {
				continue
			}
			path := filepath.Join(dir, e.Name())
			if !isExecutable(path) {
				continue
			}
			seen[name] = true
			plugins = append(plugins, Plugin{Name: name, Path: path})
		}
	}
	sort.Slice(plugins, func(i, j int) bool { return plugins[i].Name < plugins[j].Name })
	return plugins
}

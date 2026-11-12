package appdir

import (
	"os"
	"path/filepath"
)

// FormerName is what this app's folder was called before it was called
// kestrel: ~/.config/magpie, the cache folder beside it, and the files in
// them (settings, gateway keys, providers, accounts).
const FormerName = "magpie"

// AdoptFormer moves a folder of the former name to the current one when the
// current one is not there yet, so what the original Magpie wrote is found
// again rather than the new name starting empty. Called on every start: once
// the current folder is there, it does nothing.
//
// A rename that fails (another volume, a file held open) falls back to a
// copy; the old folder is left as it was either way.
func AdoptFormer() {
	if Portable() != "" {
		// portable: both names keep everything in the data folder beside the app
		return
	}
	to := Config()
	if _, err := os.Stat(to); err == nil {
		return
	}
	from := formerOf(to)
	if from == "" {
		return
	}
	if _, err := os.Stat(from); err != nil {
		return
	}
	if os.Rename(from, to) == nil {
		return
	}
	_ = copyTree(from, to)
}

// formerOf is dir with its last element, the current name, changed to
// FormerName; "" when dir isn't named after this app (an XDG_CONFIG_HOME
// ending elsewhere).
func formerOf(dir string) string {
	if filepath.Base(dir) != "kestrel" {
		return ""
	}
	return filepath.Join(filepath.Dir(dir), FormerName)
}

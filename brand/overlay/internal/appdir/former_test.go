package appdir

import (
	"os"
	"path/filepath"
	"testing"
)

// The folder the original Magpie used is taken over once, and a folder under
// the current name is never touched.
func TestAdoptFormer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	write := func(dir, name, body string) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := filepath.Join(home, ".config", FormerName)
	write(old, "settings.json", `{"theme":"dark"}`)
	write(filepath.Join(old, "providers"), "deepseek.json", `{"id":"deepseek"}`)

	AdoptFormer()

	cur := Config()
	b, err := os.ReadFile(filepath.Join(cur, "settings.json"))
	if err != nil || string(b) != `{"theme":"dark"}` {
		t.Fatalf("settings not adopted: %v %q", err, b)
	}
	if b, err := os.ReadFile(filepath.Join(cur, "providers", "deepseek.json")); err != nil || string(b) != `{"id":"deepseek"}` {
		t.Fatalf("a file in a subfolder not adopted: %v %q", err, b)
	}
	if _, err := os.Stat(old); err == nil {
		t.Fatal("the old folder is still there after a rename")
	}

	// a second start: nothing to move, and nothing inside the current folder
	// is overwritten by an old one that reappears
	write(cur, "settings.json", `{"theme":"light"}`)
	write(old, "settings.json", `{"theme":"stale"}`)
	AdoptFormer()
	if b, _ := os.ReadFile(filepath.Join(cur, "settings.json")); string(b) != `{"theme":"light"}` {
		t.Fatalf("the current folder was overwritten: %q", b)
	}
}

package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kestrel-app/kestrel/internal/edit"
	"github.com/kestrel-app/kestrel/internal/gateway"
	"github.com/kestrel-app/kestrel/internal/provider"
)

// What the original Magpie (and the first build of this shell) wrote in the
// agents' files says kestrel after one start-up, each file is copied aside
// first, and a second start-up changes nothing — nor the backup.
func TestRenameFormer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	write := func(rel, body string) {
		p := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Chat: "https://api.deepseek.com/v1", Key: "k", Models: []string{"deepseek-flash"}}); err != nil {
		t.Fatal(err)
	}
	oc := `{
  // keep me
  "provider": {
    "magpie": {"npm": "@ai-sdk/openai-compatible", "name": "magpie", "options": {"apiKey": "magpie", "baseURL": "http://127.0.0.1:3425/v1"}, "models": {}},
    "mine": {"name": "mine"}
  },
  "model": "magpie/deepseek/deepseek-flash",
  "theme": "dark"
}
`
	write(".config/opencode/opencode.jsonc", oc)
	cx := "model_reasoning_effort = \"high\"\nmodel = \"magpie/deepseek/deepseek-flash\"\nmodel_provider = \"magpie\"\nmodel_catalog_json = '" + filepath.Join(home, ".codex", "magpie-models.json") + "'\n\n[projects.\"/x\"]\ntrust_level = \"trusted\"\n\n[model_providers.magpie]\nname = \"magpie\"\nbase_url = \"http://127.0.0.1:3425/v1\"\nwire_api = \"responses\"\nexperimental_bearer_token = \"magpie\"\n"
	write(".codex/config.toml", cx)
	write(".codex/magpie-models.json", "{}")
	write(".pi/agent/settings.json", `{"defaultProvider": "magpie", "defaultModel": "deepseek/deepseek-flash", "theme": "dark"}`)
	pm := `{"providers": {"magpie": {"api": "openai-completions", "apiKey": "magpie", "baseUrl": "http://127.0.0.1:3425/v1", "models": []}}}`
	write(".pi/agent/models.json", pm)
	write(".claude/settings.json", `{"env": {"ANTHROPIC_BASE_URL": "`+gateway.URL()+`", "ANTHROPIC_AUTH_TOKEN": "magpie", "ANTHROPIC_MODEL": "deepseek/deepseek-flash"}, "theme": "dark"}`)
	// someone else's provider called magpie, on their own server: not ours
	write(".config/crush/crush.json", `{"providers": {"magpie": {"base_url": "https://api.example.net/v1"}}, "theme": "dark"}`)

	RenameFormer()

	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(home, rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if v, _ := edit.GetJSON(filepath.Join(home, ".config/opencode/opencode.jsonc"), "model"); v != "kestrel/deepseek/deepseek-flash" {
		t.Fatalf("opencode model: %q", v)
	}
	if got := read(".config/opencode/opencode.jsonc"); strings.Contains(got, `"magpie"`) || !strings.Contains(got, `"kestrel"`) || !strings.Contains(got, "// keep me") || !strings.Contains(got, `"mine"`) {
		t.Fatalf("opencode:\n%s", got)
	}
	if got := read(".codex/config.toml"); strings.Contains(got, "magpie") || !strings.Contains(got, "[model_providers.kestrel]") || !strings.Contains(got, `model_provider = "kestrel"`) || !strings.Contains(got, `[projects."/x"]`) {
		t.Fatalf("codex:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "magpie-models.json")); err == nil {
		t.Fatal("magpie-models.json still there")
	}
	if b := stashLoad(); b["codex.provider"] == "magpie" {
		t.Fatal("magpie stashed as the provider to go back to")
	}
	if v, _ := edit.GetJSON(filepath.Join(home, ".pi/agent/settings.json"), "defaultProvider"); v != "kestrel" {
		t.Fatalf("pi provider: %q", v)
	}
	if got := read(".pi/agent/models.json"); strings.Contains(got, `"magpie"`) || !strings.Contains(got, `"kestrel"`) {
		t.Fatalf("pi models:\n%s", got)
	}
	if got := read(".claude/settings.json"); strings.Contains(got, `"magpie"`) || !strings.Contains(got, `"ANTHROPIC_AUTH_TOKEN": "kestrel"`) || !strings.Contains(got, `"theme": "dark"`) {
		t.Fatalf("claude:\n%s", got)
	}
	if got := read(".config/crush/crush.json"); !strings.Contains(got, `"magpie"`) || !strings.Contains(got, "api.example.net") {
		t.Fatalf("a provider that isn't ours was rewritten:\n%s", got)
	}

	// each rewritten file was copied aside, once
	for _, rel := range []string{".config/opencode/opencode.jsonc", ".codex/config.toml", ".pi/agent/settings.json", ".pi/agent/models.json", ".claude/settings.json"} {
		b, err := os.ReadFile(filepath.Join(home, rel+formerBackup))
		if err != nil {
			t.Fatalf("%s: no backup: %v", rel, err)
		}
		if !strings.Contains(string(b), formerID) {
			t.Fatalf("%s: backup isn't the file before the rewrite", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".config/crush/crush.json"+formerBackup)); err == nil {
		t.Fatal("a file left alone was backed up")
	}

	// a second run changes neither the files nor the backups
	before := map[string]string{}
	for _, rel := range []string{".config/opencode/opencode.jsonc", ".codex/config.toml", ".pi/agent/settings.json", ".pi/agent/models.json", ".claude/settings.json"} {
		before[rel] = read(rel)
		before[rel+formerBackup] = read(rel + formerBackup)
	}
	RenameFormer()
	for rel, b := range before {
		if read(rel) != b {
			t.Fatalf("%s changed on the second run", rel)
		}
	}
}

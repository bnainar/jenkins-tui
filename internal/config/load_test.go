package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadValidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jenkins.yaml")
	content := `
jenkins:
  - id: prod
    host: https://jenkins.example.com
    username: ci-user
    credential:
      type: keyring
      ref: jenkins-tui/prod
`
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Jenkins) != 1 {
		t.Fatalf("expected 1 target, got %d", len(cfg.Jenkins))
	}
	if cfg.Jenkins[0].ID != "prod" {
		t.Fatalf("expected ID prod, got %q", cfg.Jenkins[0].ID)
	}
	if cfg.Jenkins[0].Name != "prod" {
		t.Fatalf("expected name to default to ID prod, got %q", cfg.Jenkins[0].Name)
	}
}

func TestLoadRejectsInvalidCredentialType(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jenkins.yaml")
	content := `
jenkins:
  - id: prod
    host: https://jenkins.example.com
    username: ci-user
    credential:
      type: file
      ref: some-ref
`
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "credential.type") {
		t.Fatalf("expected credential.type error, got %v", err)
	}
}

func TestResolvePathPrecedence(t *testing.T) {
	t.Setenv("JENKINS_TUI_CONFIG", "/tmp/from-env.yaml")
	got, err := ResolvePath("/tmp/from-flag.yaml")
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if got != "/tmp/from-flag.yaml" {
		t.Fatalf("expected flag path, got %q", got)
	}

	got, err = ResolvePath("")
	if err != nil {
		t.Fatalf("ResolvePath env fallback: %v", err)
	}
	if got != "/tmp/from-env.yaml" {
		t.Fatalf("expected env path, got %q", got)
	}
}

func TestResolvePathRejectsRelative(t *testing.T) {
	if _, err := ResolvePath("relative.yaml"); err == nil {
		t.Fatalf("expected error for relative path")
	}
}

func TestResolveCacheDirPrecedence(t *testing.T) {
	t.Setenv("JENKINS_TUI_CACHE_DIR", "/tmp/cache-env")
	got, err := ResolveCacheDir("/tmp/cache-flag")
	if err != nil {
		t.Fatalf("ResolveCacheDir: %v", err)
	}
	if got != "/tmp/cache-flag" {
		t.Fatalf("expected flag cache dir, got %q", got)
	}
	got, err = ResolveCacheDir("")
	if err != nil {
		t.Fatalf("ResolveCacheDir env fallback: %v", err)
	}
	if got != "/tmp/cache-env" {
		t.Fatalf("expected env cache dir, got %q", got)
	}
}

func TestResolveCacheDirRejectsRelative(t *testing.T) {
	if _, err := ResolveCacheDir("cache"); err == nil {
		t.Fatalf("expected error for relative cache dir")
	}
}

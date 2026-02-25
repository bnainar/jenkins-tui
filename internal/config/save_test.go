package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jenkins-tui/internal/models"
)

func TestSaveWritesConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "jenkins.yaml")
	cfg := models.Config{
		Jenkins: []models.JenkinsTarget{
			{
				ID:       "prod",
				Name:     "prod",
				Host:     "https://jenkins.example.com",
				Username: "ci-user",
				Credential: models.Credential{
					Type: models.CredentialTypeKeyring,
					Ref:  "jenkins-tui/prod",
				},
			},
		},
	}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	text := string(b)
	if !strings.Contains(text, "credential:") {
		t.Fatalf("expected credential block in config, got: %s", text)
	}
	if strings.Contains(text, "timeout") || strings.Contains(text, "cache_dir") {
		t.Fatalf("runtime-only fields should not be persisted")
	}
}

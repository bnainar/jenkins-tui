package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"jenkins-tui/internal/models"
)

func Load(path string) (models.Config, error) {
	var cfg models.Config
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	seenIDs := map[string]struct{}{}
	for i, t := range cfg.Jenkins {
		if strings.TrimSpace(t.ID) == "" {
			return cfg, fmt.Errorf("jenkins[%d].id is required", i)
		}
		id := strings.TrimSpace(t.ID)
		if _, ok := seenIDs[id]; ok {
			return cfg, fmt.Errorf("jenkins[%d].id %q is duplicated", i, id)
		}
		seenIDs[id] = struct{}{}
		if strings.TrimSpace(t.Host) == "" {
			return cfg, fmt.Errorf("jenkins[%d].host is required", i)
		}
		if strings.TrimSpace(t.Username) == "" {
			return cfg, fmt.Errorf("jenkins[%d].username is required", i)
		}
		if t.Credential.Type != models.CredentialTypeKeyring && t.Credential.Type != models.CredentialTypeEnv {
			return cfg, fmt.Errorf("jenkins[%d].credential.type must be %q or %q", i, models.CredentialTypeKeyring, models.CredentialTypeEnv)
		}
		if strings.TrimSpace(t.Credential.Ref) == "" {
			return cfg, fmt.Errorf("jenkins[%d].credential.ref is required", i)
		}
		if strings.TrimSpace(t.Name) == "" {
			cfg.Jenkins[i].Name = t.Host
		}
		cfg.Jenkins[i].ID = id
		cfg.Jenkins[i].Host = strings.TrimRight(strings.TrimSpace(t.Host), "/")
		cfg.Jenkins[i].Username = strings.TrimSpace(t.Username)
		cfg.Jenkins[i].Credential.Ref = strings.TrimSpace(t.Credential.Ref)
	}
	return cfg, nil
}

func ResolvePath(flagPath string) (string, error) {
	path := strings.TrimSpace(flagPath)
	if path == "" {
		path = strings.TrimSpace(os.Getenv("JENKINS_TUI_CONFIG"))
	}
	if path != "" {
		if !filepath.IsAbs(path) {
			return "", fmt.Errorf("config path must be absolute: %s", path)
		}
		return path, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(base, "jenkins-tui", "jenkins.yaml"), nil
}

func ResolveCacheDir(flagDir string) (string, error) {
	path := strings.TrimSpace(flagDir)
	if path == "" {
		path = strings.TrimSpace(os.Getenv("JENKINS_TUI_CACHE_DIR"))
	}
	if path != "" {
		if !filepath.IsAbs(path) {
			return "", fmt.Errorf("cache dir must be absolute: %s", path)
		}
		return path, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache dir: %w", err)
	}
	return filepath.Join(base, "jenkins-tui"), nil
}

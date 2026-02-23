package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"jenx/internal/models"
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
	for i, t := range cfg.Jenkins {
		if strings.TrimSpace(t.Host) == "" {
			return cfg, fmt.Errorf("jenkins[%d].host is required", i)
		}
		if strings.TrimSpace(t.Username) == "" {
			return cfg, fmt.Errorf("jenkins[%d].username is required", i)
		}
		if strings.TrimSpace(t.Token) == "" {
			return cfg, fmt.Errorf("jenkins[%d].token is required", i)
		}
		if strings.TrimSpace(t.Name) == "" {
			cfg.Jenkins[i].Name = t.Host
		}
	}
	return cfg, nil
}

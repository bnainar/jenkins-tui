package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"jenkins-tui/internal/config"
	"jenkins-tui/internal/models"
	"jenkins-tui/internal/tui"
)

func main() {
	configPathFlag := flag.String("config", "", "absolute path to jenkins config file (default: $JENKINS_TUI_CONFIG or XDG config path)")
	cacheDirFlag := flag.String("cache-dir", "", "absolute path for jobs cache (default: $JENKINS_TUI_CACHE_DIR or XDG cache path)")
	timeout := flag.Duration("timeout", 60*time.Second, "HTTP client timeout for Jenkins API requests")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	configPath, err := config.ResolvePath(*configPathFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}
	cacheDir, err := config.ResolveCacheDir(*cacheDirFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	cfg, err := config.Load(configPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}
	if errors.Is(err, os.ErrNotExist) {
		cfg = models.Config{}
	}
	cfg.Timeout = *timeout
	cfg.ConfigPath = configPath
	cfg.CacheDir = cacheDir

	model := tui.NewModel(ctx, cfg)
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "runtime error: %v\n", err)
		os.Exit(1)
	}
}

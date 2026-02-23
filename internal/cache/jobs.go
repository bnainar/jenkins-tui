package cache

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"jenx/internal/models"
)

const (
	jobsTTL = 24 * time.Hour
)

type jobsCacheFile struct {
	FetchedAt time.Time       `json:"fetched_at"`
	Jobs      []models.JobRef `json:"jobs"`
}

func Jobs(cacheKey string) ([]models.JobRef, bool, error) {
	path, err := jobsPath(cacheKey)
	if err != nil {
		return nil, false, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var f jobsCacheFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, false, err
	}
	if f.FetchedAt.IsZero() || time.Since(f.FetchedAt) > jobsTTL {
		return nil, false, nil
	}
	return f.Jobs, true, nil
}

func SaveJobs(cacheKey string, jobs []models.JobRef) error {
	path, err := jobsPath(cacheKey)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload := jobsCacheFile{
		FetchedAt: time.Now().UTC(),
		Jobs:      jobs,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func jobsPath(cacheKey string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache dir: %w", err)
	}
	sum := sha1.Sum([]byte(cacheKey))
	file := "jobs_" + hex.EncodeToString(sum[:]) + ".json"
	return filepath.Join(base, "jenx", file), nil
}

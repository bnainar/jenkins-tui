package credentials

import (
	"fmt"
	"os"
	"strings"
)

type EnvStore struct{}

func NewEnvStore() *EnvStore {
	return &EnvStore{}
}

func (s *EnvStore) Get(ref string) (string, error) {
	key := strings.TrimSpace(ref)
	if key == "" {
		return "", fmt.Errorf("credential ref is required")
	}
	value := os.Getenv(key)
	if value == "" {
		return "", ErrNotFound
	}
	return value, nil
}

func (s *EnvStore) Set(ref, value string) error {
	return fmt.Errorf("cannot set env credentials at runtime")
}

func (s *EnvStore) Delete(ref string) error {
	return fmt.Errorf("cannot delete env credentials at runtime")
}

func (s *EnvStore) Available() (bool, error) {
	return true, nil
}

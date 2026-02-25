package credentials

import (
	"errors"
	"fmt"
	"strings"

	"github.com/zalando/go-keyring"
)

const serviceName = "com.bnainar.jenkins-tui"

type KeyringStore struct {
	Service string
}

func NewKeyringStore() *KeyringStore {
	return &KeyringStore{Service: serviceName}
}

func (s *KeyringStore) Get(ref string) (string, error) {
	value, err := keyring.Get(s.service(), strings.TrimSpace(ref))
	if err == nil {
		return value, nil
	}
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	return "", err
}

func (s *KeyringStore) Set(ref, value string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return fmt.Errorf("credential ref is required")
	}
	if value == "" {
		return fmt.Errorf("credential value is required")
	}
	return keyring.Set(s.service(), ref, value)
}

func (s *KeyringStore) Delete(ref string) error {
	err := keyring.Delete(s.service(), strings.TrimSpace(ref))
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

func (s *KeyringStore) Available() (bool, error) {
	ref := "__jenkins_tui_probe__"
	value, err := keyring.Get(s.service(), ref)
	if err == nil {
		if value != "" {
			return true, nil
		}
		return true, nil
	}
	if errors.Is(err, keyring.ErrNotFound) {
		return true, nil
	}
	return false, err
}

func (s *KeyringStore) service() string {
	if strings.TrimSpace(s.Service) != "" {
		return s.Service
	}
	return serviceName
}

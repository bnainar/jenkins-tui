package credentials

import (
	"errors"
	"fmt"
	"strings"

	"jenkins-tui/internal/models"
)

type Manager struct {
	keyring Store
	env     Store
}

func NewManager() *Manager {
	return &Manager{
		keyring: NewKeyringStore(),
		env:     NewEnvStore(),
	}
}

func (m *Manager) Resolve(target models.JenkinsTarget) (string, error) {
	switch target.Credential.Type {
	case models.CredentialTypeKeyring:
		token, err := m.keyring.Get(target.Credential.Ref)
		if err == nil {
			return token, nil
		}
		if errors.Is(err, ErrNotFound) {
			return "", fmt.Errorf("keyring credential %q not found for target %q", target.Credential.Ref, target.Name)
		}
		return "", fmt.Errorf("read keyring credential %q for target %q: %w", target.Credential.Ref, target.Name, err)
	case models.CredentialTypeEnv:
		token, err := m.env.Get(target.Credential.Ref)
		if err == nil {
			return token, nil
		}
		if errors.Is(err, ErrNotFound) {
			return "", fmt.Errorf("env credential %q not found for target %q", target.Credential.Ref, target.Name)
		}
		return "", fmt.Errorf("read env credential %q for target %q: %w", target.Credential.Ref, target.Name, err)
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedType, target.Credential.Type)
	}
}

func (m *Manager) SetKeyring(ref, value string) error {
	return m.keyring.Set(strings.TrimSpace(ref), value)
}

func (m *Manager) DeleteKeyring(ref string) error {
	return m.keyring.Delete(strings.TrimSpace(ref))
}

func (m *Manager) KeyringAvailable() (bool, error) {
	return m.keyring.Available()
}

package credentials

import "testing"

func TestEnvStoreGet(t *testing.T) {
	t.Setenv("JENKINS_TUI_TEST_TOKEN", "abc123")
	store := NewEnvStore()
	got, err := store.Get("JENKINS_TUI_TEST_TOKEN")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "abc123" {
		t.Fatalf("expected token, got %q", got)
	}
}

func TestEnvStoreGetMissing(t *testing.T) {
	store := NewEnvStore()
	if _, err := store.Get("JENKINS_TUI_TEST_TOKEN_MISSING"); err == nil {
		t.Fatalf("expected error for missing env credential")
	}
}

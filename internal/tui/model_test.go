package tui

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"jenkins-tui/internal/jenkins"
	"jenkins-tui/internal/models"
)

func TestParamsStatusMessageMentionsCtrlA(t *testing.T) {
	msg := paramsStatusMessage()
	if !strings.Contains(msg, "ctrl+a") {
		t.Fatalf("expected status message to mention ctrl+a, got %q", msg)
	}
	if !strings.Contains(msg, "select all/none") {
		t.Fatalf("expected status message to mention select all/none, got %q", msg)
	}
}

func TestHelpTextForScreenParamsMentionsSelectAll(t *testing.T) {
	help := helpTextForScreen(screenParams, false, true)
	if !strings.Contains(help, "ctrl+a: select all/none") {
		t.Fatalf("expected params help to mention ctrl+a select all/none, got %q", help)
	}
}

func TestHelpTextForScreenDoneAddsRerun(t *testing.T) {
	help := helpTextForScreen(screenDone, true, true)
	if !strings.Contains(help, "r: rerun failed") {
		t.Fatalf("expected done help to include rerun shortcut, got %q", help)
	}
}

func TestHelpTextForScreenServersMentionsServerActions(t *testing.T) {
	help := helpTextForScreen(screenServers, false, true)
	for _, want := range []string{"a/m: add", "e: edit", "t: rotate token", "d: delete"} {
		if !strings.Contains(help, want) {
			t.Fatalf("expected servers help to include %q, got %q", want, help)
		}
	}
}

func TestHelpTextForScreenJobsMentionsGlobalSearch(t *testing.T) {
	help := helpTextForScreen(screenJobs, false, true)
	if !strings.Contains(help, "g: global search") {
		t.Fatalf("expected jobs help to include global search shortcut, got %q", help)
	}
}

func TestHelpTextCompactMode(t *testing.T) {
	help := helpTextForScreen(screenGlobalSearch, false, false)
	if !strings.Contains(help, "type search") {
		t.Fatalf("expected compact global search help, got %q", help)
	}
}

func TestJobsLoadedKeepsStaticTitle(t *testing.T) {
	m, ok := NewModel(context.Background(), models.Config{Timeout: time.Second}).(*model)
	if !ok {
		t.Fatalf("NewModel should return *model")
	}
	m.jobsReqID = 1
	updated, _ := m.Update(jobsLoadedMsg{
		requestID: 1,
		prefix:    "folder",
		nodes: []models.JobNode{
			{Name: "job1", FullName: "folder/job1", URL: "https://jenkins/job/folder/job/job1/", Kind: models.JobNodeJob},
		},
	})
	m = updated.(*model)
	if got := m.jobs.Title; got != "Browse Jenkins Jobs" {
		t.Fatalf("expected static jobs title, got %q", got)
	}
}

func TestJobsViewShowsPathBreadcrumb(t *testing.T) {
	m, ok := NewModel(context.Background(), models.Config{Timeout: time.Second}).(*model)
	if !ok {
		t.Fatalf("NewModel should return *model")
	}
	m.width = 120
	m.height = 40
	m.screen = screenJobs
	m.client = jenkins.NewClient(models.JenkinsTarget{Host: "https://jenkins.example.com"}, "token", time.Second)
	m.jobFolders = []models.JobNode{{FullName: "folder/subfolder"}}
	view := m.View()
	if !strings.Contains(view, "Path: /folder/subfolder") {
		t.Fatalf("expected jobs view breadcrumb path, got %q", view)
	}
}

func TestParamsViewShowsSelectedJobFullPath(t *testing.T) {
	m, ok := NewModel(context.Background(), models.Config{Timeout: time.Second}).(*model)
	if !ok {
		t.Fatalf("NewModel should return *model")
	}
	m.width = 120
	m.height = 40
	m.screen = screenParams
	m.selectedJob = &models.JobRef{Name: "job1", FullName: "folder/job1"}
	value := ""
	m.paramForm = huh.NewForm(huh.NewGroup(huh.NewInput().Title("email").Value(&value)))
	view := m.View()
	if !strings.Contains(view, "Job: /folder/job1") {
		t.Fatalf("expected params view to show full job path, got %q", view)
	}
}

func TestParamsViewFallsBackToJobName(t *testing.T) {
	m, ok := NewModel(context.Background(), models.Config{Timeout: time.Second}).(*model)
	if !ok {
		t.Fatalf("NewModel should return *model")
	}
	m.width = 120
	m.height = 40
	m.screen = screenParams
	m.selectedJob = &models.JobRef{Name: "my-job"}
	value := ""
	m.paramForm = huh.NewForm(huh.NewGroup(huh.NewInput().Title("email").Value(&value)))
	view := m.View()
	if !strings.Contains(view, "Job: my-job") {
		t.Fatalf("expected params view to fall back to job name, got %q", view)
	}
}

func TestApplySelectedStylesKeepsBorderColorConsistent(t *testing.T) {
	d := list.NewDefaultDelegate()
	applySelectedStyles(&d)

	titleBorder := d.Styles.SelectedTitle.GetBorderLeftForeground()
	descBorder := d.Styles.SelectedDesc.GetBorderLeftForeground()
	if !reflect.DeepEqual(titleBorder, descBorder) {
		t.Fatalf("expected matching selected border colors, title=%v desc=%v", titleBorder, descBorder)
	}

	if !d.Styles.SelectedDesc.GetBorderLeft() {
		t.Fatalf("expected selected description to keep left border")
	}

	if fg := d.Styles.SelectedDesc.GetForeground(); !reflect.DeepEqual(fg, lipgloss.Color("245")) {
		t.Fatalf("expected selected description foreground to be 245, got %v", fg)
	}
}

func TestEscInJobsNavigatesBackNotQuit(t *testing.T) {
	m, ok := NewModel(context.Background(), models.Config{Timeout: time.Second}).(*model)
	if !ok {
		t.Fatalf("NewModel should return *model")
	}
	m.screen = screenJobs
	m.jobFolders = []models.JobNode{{Name: "folder", FullName: "folder"}}
	m.client = nil

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(*model)

	if len(m.jobFolders) != 0 {
		t.Fatalf("expected esc to navigate up one level, got %d folders", len(m.jobFolders))
	}

	if cmd != nil {
		if msg := cmd(); msg != nil {
			if _, quit := msg.(tea.QuitMsg); quit {
				t.Fatalf("esc should not trigger tea.Quit")
			}
		}
	}
}

func TestNewModelWithNoServersStartsAddServerForm(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "jenkins.yaml")
	m, ok := NewModel(context.Background(), models.Config{
		ConfigPath: cfgPath,
		Timeout:    time.Second,
	}).(*model)
	if !ok {
		t.Fatalf("NewModel should return *model")
	}
	if m.screen != screenManageForm {
		t.Fatalf("expected screenManageForm on empty server list, got %v", m.screen)
	}
	if m.manageMode != manageModeAdd {
		t.Fatalf("expected manage mode add, got %v", m.manageMode)
	}
	if m.manageForm == nil {
		t.Fatalf("expected add server form to be initialized")
	}
	if !strings.Contains(m.status, "No Jenkins servers configured") {
		t.Fatalf("expected no-server status, got %q", m.status)
	}
}

func TestManageFormAcceptsTypingAfterInit(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "jenkins.yaml")
	m, ok := NewModel(context.Background(), models.Config{
		ConfigPath: cfgPath,
		Timeout:    time.Second,
	}).(*model)
	if !ok {
		t.Fatalf("NewModel should return *model")
	}
	if m.manageForm == nil {
		t.Fatalf("expected manage form to be initialized")
	}

	if cmd := m.Init(); cmd != nil {
		if msg := cmd(); msg != nil {
			updated, _ := m.Update(msg)
			m = updated.(*model)
		}
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h"), Alt: false})
	m = updated.(*model)
	if got := strings.TrimSpace(m.manageHost); got != "h" {
		t.Fatalf("expected typed input to populate Jenkins URL field, got %q", got)
	}
}

func TestAllowQuickQuitDisabledOnFormScreens(t *testing.T) {
	m := &model{}
	m.screen = screenParams
	if m.allowQuickQuit() {
		t.Fatalf("expected quick quit to be disabled on params screen")
	}
	m.screen = screenManageForm
	if m.allowQuickQuit() {
		t.Fatalf("expected quick quit to be disabled on manage form screen")
	}
}

func TestDeriveNameFromHost(t *testing.T) {
	tests := []struct {
		host string
		want string
	}{
		{host: "https://jenkins.example.com", want: "jenkins.example.com"},
		{host: "http://jenkins.example.com:8080/root", want: "jenkins.example.com"},
		{host: "jenkins.internal.local:8443/folder", want: "jenkins.internal.local"},
	}
	for _, tc := range tests {
		got := deriveNameFromHost(tc.host)
		if got != tc.want {
			t.Fatalf("deriveNameFromHost(%q) = %q, want %q", tc.host, got, tc.want)
		}
	}
}

func TestSlugifyID(t *testing.T) {
	got := slugifyID(" Prod Jenkins !! ")
	want := "prod-jenkins"
	if got != want {
		t.Fatalf("slugifyID = %q, want %q", got, want)
	}
}

func TestUniqueAutoID(t *testing.T) {
	m := &model{
		cfg: models.Config{
			Jenkins: []models.JenkinsTarget{
				{ID: "prod"},
				{ID: "prod-2"},
			},
		},
	}
	got := m.uniqueAutoID("prod", nil)
	if got != "prod-3" {
		t.Fatalf("uniqueAutoID = %q, want %q", got, "prod-3")
	}
}

func TestDefaultKeyringRef(t *testing.T) {
	got := defaultKeyringRef("Prod Main")
	if got != "jenkins-tui/prod-main" {
		t.Fatalf("defaultKeyringRef = %q", got)
	}
}

func TestApplyManageFormAddKeyringSuccess(t *testing.T) {
	creds := newStubCreds()
	m := newTestManageModel(t, creds)
	m.manageMode = manageModeAdd
	m.manageHost = "https://jenkins.example.com"
	m.manageUsername = "ci-user"
	m.manageTokenSrc = tokenStorageKeyring
	m.manageToken = "api-token-123"
	m.keyringAvail = true

	var validatedToken string
	m.validateTarget = func(ctx context.Context, target models.JenkinsTarget, token string, timeout time.Duration) error {
		validatedToken = token
		return nil
	}

	if err := m.applyManageForm(); err != nil {
		t.Fatalf("applyManageForm: %v", err)
	}
	if validatedToken != "api-token-123" {
		t.Fatalf("expected typed token to be validated, got %q", validatedToken)
	}
	if len(m.cfg.Jenkins) != 1 {
		t.Fatalf("expected 1 server, got %d", len(m.cfg.Jenkins))
	}
	got := m.cfg.Jenkins[0]
	if got.Credential.Type != models.CredentialTypeKeyring {
		t.Fatalf("expected keyring credential type, got %q", got.Credential.Type)
	}
	if got.Credential.Ref != "jenkins-tui/jenkins-example-com" {
		t.Fatalf("expected default keyring ref, got %q", got.Credential.Ref)
	}
	if creds.setCount != 1 {
		t.Fatalf("expected keyring set once, got %d", creds.setCount)
	}
}

func TestApplyManageFormEnvVarMissingBlocksSave(t *testing.T) {
	creds := newStubCreds()
	m := newTestManageModel(t, creds)
	m.manageMode = manageModeAdd
	m.manageHost = "https://jenkins.example.com"
	m.manageUsername = "ci-user"
	m.manageTokenSrc = tokenStorageEnv
	m.manageEnvVar = "JENKINS_TOKEN_PROD"
	m.lookupEnv = func(string) string { return "" }

	validateCalled := false
	m.validateTarget = func(ctx context.Context, target models.JenkinsTarget, token string, timeout time.Duration) error {
		validateCalled = true
		return nil
	}

	err := m.applyManageForm()
	if err == nil {
		t.Fatalf("expected env var validation error")
	}
	if err.Error() != "Environment variable JENKINS_TOKEN_PROD is not set or empty." {
		t.Fatalf("unexpected error: %v", err)
	}
	if validateCalled {
		t.Fatalf("validateTarget should not be called when env var is missing")
	}
	if len(m.cfg.Jenkins) != 0 {
		t.Fatalf("server should not be saved on failure")
	}
}

func TestApplyManageFormValidationFailureDoesNotWriteKeyring(t *testing.T) {
	creds := newStubCreds()
	m := newTestManageModel(t, creds)
	m.manageMode = manageModeAdd
	m.manageHost = "https://jenkins.example.com"
	m.manageUsername = "ci-user"
	m.manageTokenSrc = tokenStorageKeyring
	m.manageToken = "api-token-123"
	m.keyringAvail = true
	m.validateTarget = func(ctx context.Context, target models.JenkinsTarget, token string, timeout time.Duration) error {
		return errors.New("GET https://jenkins.example.com/api/json failed (401): unauthorized")
	}

	err := m.applyManageForm()
	if err == nil {
		t.Fatalf("expected validation failure")
	}
	if err.Error() != "Authentication failed. Check username and API token." {
		t.Fatalf("unexpected mapped error: %v", err)
	}
	if creds.setCount != 0 {
		t.Fatalf("keyring should not be written when validation fails")
	}
	if len(m.cfg.Jenkins) != 0 {
		t.Fatalf("server should not be saved on validation failure")
	}
}

func TestApplyManageFormEditReusesExistingKeyringToken(t *testing.T) {
	creds := newStubCreds()
	creds.values["jenkins-tui/prod"] = "existing-token"

	m := newTestManageModel(t, creds)
	m.cfg.Jenkins = []models.JenkinsTarget{
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
	}
	m.manageMode = manageModeEdit
	m.manageIndex = 0
	m.manageHost = "https://jenkins.example.com"
	m.manageUsername = "ci-user"
	m.manageName = "prod"
	m.manageTokenSrc = tokenStorageKeyring
	m.manageToken = ""
	m.keyringAvail = true

	var validatedToken string
	m.validateTarget = func(ctx context.Context, target models.JenkinsTarget, token string, timeout time.Duration) error {
		validatedToken = token
		return nil
	}

	if err := m.applyManageForm(); err != nil {
		t.Fatalf("applyManageForm edit: %v", err)
	}
	if validatedToken != "existing-token" {
		t.Fatalf("expected existing token to be reused, got %q", validatedToken)
	}
	if creds.setCount != 0 {
		t.Fatalf("expected no keyring write when token is reused")
	}
}

func TestApplyManageFormEditChangedKeyringRefRequiresToken(t *testing.T) {
	creds := newStubCreds()
	creds.values["jenkins-tui/prod"] = "existing-token"

	m := newTestManageModel(t, creds)
	m.cfg.Jenkins = []models.JenkinsTarget{
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
	}
	m.manageMode = manageModeEdit
	m.manageIndex = 0
	m.manageHost = "https://jenkins.example.com"
	m.manageUsername = "ci-user"
	m.manageName = "prod"
	m.manageTokenSrc = tokenStorageKeyring
	m.manageToken = ""
	m.manageKeyRef = "jenkins-tui/prod-new"
	m.manageAdvanced = true
	m.keyringAvail = true

	validateCalled := false
	m.validateTarget = func(ctx context.Context, target models.JenkinsTarget, token string, timeout time.Duration) error {
		validateCalled = true
		return nil
	}

	err := m.applyManageForm()
	if err == nil {
		t.Fatalf("expected missing token error")
	}
	if err.Error() != "API token is required." {
		t.Fatalf("unexpected error: %v", err)
	}
	if validateCalled {
		t.Fatalf("validation should not run when required token is missing")
	}
	if creds.setCount != 0 {
		t.Fatalf("keyring should not be written")
	}
}

type stubCreds struct {
	values   map[string]string
	setCount int
	avail    bool
}

func newStubCreds() *stubCreds {
	return &stubCreds{
		values: map[string]string{},
		avail:  true,
	}
}

func (s *stubCreds) Resolve(target models.JenkinsTarget) (string, error) {
	if target.Credential.Type == models.CredentialTypeEnv {
		return "", errors.New("not implemented for env in tests")
	}
	val, ok := s.values[target.Credential.Ref]
	if !ok || strings.TrimSpace(val) == "" {
		return "", errors.New("credential not found")
	}
	return val, nil
}

func (s *stubCreds) SetKeyring(ref, value string) error {
	s.values[ref] = value
	s.setCount++
	return nil
}

func (s *stubCreds) DeleteKeyring(ref string) error {
	delete(s.values, ref)
	return nil
}

func (s *stubCreds) KeyringAvailable() (bool, error) {
	return s.avail, nil
}

func newTestManageModel(t *testing.T, creds credentialsManager) *model {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "jenkins.yaml")
	return &model{
		ctx:            context.Background(),
		cfg:            models.Config{ConfigPath: cfgPath, Timeout: time.Second},
		creds:          creds,
		manageIndex:    -1,
		manageTokenSrc: tokenStorageKeyring,
		manageInsecure: "false",
		keyringAvail:   true,
		lookupEnv:      func(string) string { return "" },
		validateTarget: func(ctx context.Context, target models.JenkinsTarget, token string, timeout time.Duration) error {
			return nil
		},
	}
}

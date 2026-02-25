package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"jenkins-tui/internal/browser"
	"jenkins-tui/internal/cache"
	"jenkins-tui/internal/config"
	"jenkins-tui/internal/credentials"
	"jenkins-tui/internal/executor"
	"jenkins-tui/internal/jenkins"
	"jenkins-tui/internal/models"
	"jenkins-tui/internal/permutation"
	"jenkins-tui/internal/ui"
)

type screen int

const (
	screenServers screen = iota
	screenJobs
	screenParams
	screenPreview
	screenRun
	screenDone
	screenManageTargets
	screenManageForm
)

const (
	maxPermutations = 20
	concurrencyCap  = 4
)

type listItem struct {
	title    string
	desc     string
	id       string
	name     string
	fullName string
	kind     models.JobNodeKind
}

func (i listItem) Title() string       { return i.title }
func (i listItem) Description() string { return i.desc }
func (i listItem) FilterValue() string {
	return strings.TrimSpace(i.title + " " + i.desc + " " + i.fullName)
}

type jobsLoadedMsg struct {
	nodes        []models.JobNode
	fromCache    bool
	err          error
	requestID    uint64
	containerURL string
	prefix       string
}

type paramsLoadedMsg struct {
	params []models.ParamDef
	err    error
}

type runStreamStartedMsg struct {
	ch <-chan models.RunUpdate
}

type runEventMsg struct {
	update models.RunUpdate
}

type runDoneMsg struct{}

type manageMode int

const (
	manageModeAdd manageMode = iota
	manageModeEdit
	manageModeRotate
)

type model struct {
	ctx   context.Context
	cfg   models.Config
	creds *credentials.Manager

	width  int
	height int

	screen       screen
	err          error
	status       string
	loading      bool
	loadingStart time.Time
	loadingLabel string

	servers list.Model
	jobs    list.Model
	manage  list.Model

	target      *models.JenkinsTarget
	client      *jenkins.Client
	selectedJob *models.JobRef
	jobFolders  []models.JobNode
	jobsReqID   uint64

	params       []models.ParamDef
	paramForm    *huh.Form
	choiceVars   map[string]*[]string
	fixedVars    map[string]*string
	permutations []models.JobSpec
	previewTable table.Model
	runRecords   []models.RunRecord
	runTable     table.Model
	finished     map[int]bool
	runEvents    <-chan models.RunUpdate
	runCtx       context.Context
	runCancel    context.CancelFunc

	manageForm     *huh.Form
	manageMode     manageMode
	manageIndex    int
	manageID       string
	manageHost     string
	manageUsername string
	manageInsecure string
	manageCredType string
	manageCredRef  string
	manageToken    string

	spin spinner.Model
}

func NewModel(ctx context.Context, cfg models.Config) tea.Model {
	servers := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	servers.Title = "Select Jenkins"
	servers.SetFilteringEnabled(true)

	jobs := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	jobs.Title = "Browse Jenkins Jobs"
	jobs.SetFilteringEnabled(true)

	manage := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	manage.Title = "Manage Jenkins Targets"
	manage.SetFilteringEnabled(true)

	spin := spinner.New()
	spin.Spinner = spinner.Dot
	m := &model{
		ctx:            ctx,
		cfg:            cfg,
		creds:          credentials.NewManager(),
		screen:         screenServers,
		servers:        servers,
		jobs:           jobs,
		manage:         manage,
		choiceVars:     map[string]*[]string{},
		fixedVars:      map[string]*string{},
		finished:       map[int]bool{},
		spin:           spin,
		manageInsecure: "false",
		manageCredType: string(models.CredentialTypeKeyring),
		manageIndex:    -1,
	}
	m.refreshServerItems()
	m.refreshManageItems()
	if len(cfg.Jenkins) == 0 {
		m.status = "No Jenkins targets configured. Press m to add one."
	}
	return m
}

func (m *model) Init() tea.Cmd {
	return m.spin.Tick
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.spin, cmd = m.spin.Update(msg)
	cmds := []tea.Cmd{cmd}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.servers.SetSize(max(0, msg.Width-8), max(0, msg.Height-10))
		m.jobs.SetSize(max(0, msg.Width-8), max(0, msg.Height-10))
		m.manage.SetSize(max(0, msg.Width-8), max(0, msg.Height-10))
		if m.paramForm != nil {
			m.paramForm.WithWidth(max(1, msg.Width-8))
		}
		if m.manageForm != nil {
			m.manageForm.WithWidth(max(1, msg.Width-8))
		}
		if len(m.permutations) > 0 {
			m.buildPreviewTable()
		}
		if len(m.runRecords) > 0 {
			m.refreshRunTable()
		}
		m.previewTable.SetHeight(max(5, msg.Height-14))
		m.runTable.SetHeight(max(5, msg.Height-14))
		cmds = append(cmds, tea.ClearScreen)
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			if m.runCancel != nil {
				m.runCancel()
			}
			return m, tea.Quit
		}
		if msg.String() == "q" {
			if m.runCancel != nil {
				m.runCancel()
			}
			return m, tea.Quit
		}
	}

	switch typed := msg.(type) {
	case jobsLoadedMsg:
		if typed.requestID != m.jobsReqID {
			return m, tea.Batch(cmds...)
		}
		m.loading = false
		if typed.err != nil {
			m.err = typed.err
			m.status = fmt.Sprintf("Failed to load %s", jobsPathLabel(typed.prefix))
			return m, tea.Batch(cmds...)
		}
		m.err = nil
		if typed.fromCache {
			m.status = fmt.Sprintf("Loaded %d items from %s (cache, TTL 24h)", len(typed.nodes), jobsPathLabel(typed.prefix))
		} else {
			m.status = fmt.Sprintf("Loaded %d items from %s", len(typed.nodes), jobsPathLabel(typed.prefix))
		}
		items := make([]list.Item, 0, len(typed.nodes))
		for _, n := range typed.nodes {
			title := n.Name
			desc := "job"
			if n.Kind == models.JobNodeFolder {
				title += "/"
				desc = "folder"
			}
			items = append(items, listItem{
				title:    title,
				desc:     desc,
				id:       n.URL,
				name:     n.Name,
				fullName: n.FullName,
				kind:     n.Kind,
			})
		}
		m.jobs.SetItems(items)
		m.jobs.Title = "Browse Jenkins Jobs: " + jobsPathLabel(typed.prefix)
		return m, m.transition(screenJobs, cmds...)
	case paramsLoadedMsg:
		m.loading = false
		if typed.err != nil {
			m.err = typed.err
			m.status = "Failed to load parameters"
			return m, tea.Batch(cmds...)
		}
		if len(typed.params) == 0 {
			m.err = nil
			m.selectedJob = nil
			m.status = "Selected job is not parameterized or has unsupported parameter types"
			return m, tea.Batch(cmds...)
		}
		m.params = typed.params
		m.buildParamForm()
		m.status = paramsStatusMessage()
		return m, m.transition(screenParams, cmds...)
	case runStreamStartedMsg:
		m.runEvents = typed.ch
		return m, waitRunEventCmd(m.runEvents)
	case runEventMsg:
		m.applyRunUpdate(typed.update)
		m.refreshRunTable()
		if typed.update.Done && !m.finished[typed.update.Index] {
			m.finished[typed.update.Index] = true
		}
		if len(m.finished) == len(m.runRecords) {
			m.status = "All jobs finished"
			return m, m.transition(screenDone, cmds...)
		}
		return m, waitRunEventCmd(m.runEvents)
	case runDoneMsg:
		if m.screen == screenRun {
			return m, m.transition(screenDone, cmds...)
		}
		return m, tea.Batch(cmds...)
	}

	switch m.screen {
	case screenServers:
		return m.updateServers(msg, cmds)
	case screenJobs:
		return m.updateJobs(msg, cmds)
	case screenParams:
		return m.updateParams(msg, cmds)
	case screenPreview:
		return m.updatePreview(msg, cmds)
	case screenRun, screenDone:
		return m.updateRun(msg, cmds)
	case screenManageTargets:
		return m.updateManageTargets(msg, cmds)
	case screenManageForm:
		return m.updateManageForm(msg, cmds)
	default:
		return m, tea.Batch(cmds...)
	}
}

func (m *model) updateServers(msg tea.Msg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.servers, cmd = m.servers.Update(msg)
	cmds = append(cmds, cmd)
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "enter":
			selected := m.servers.SelectedItem()
			item, ok := selected.(listItem)
			if !ok {
				return m, tea.Batch(cmds...)
			}
			t := m.findTargetByID(item.id)
			if t == nil {
				return m, tea.Batch(cmds...)
			}
			token, err := m.creds.Resolve(*t)
			if err != nil {
				m.err = err
				m.status = "Failed to resolve target credentials"
				return m, tea.Batch(cmds...)
			}
			m.err = nil
			m.target = t
			m.client = jenkins.NewClient(*t, token, m.cfg.Timeout)
			m.selectedJob = nil
			m.jobFolders = nil
			m.jobs.ResetFilter()
			m.jobs.SetItems(nil)
			return m, m.transition(screenJobs, append(cmds, m.loadCurrentFolderCmd(false))...)
		case "m":
			m.refreshManageItems()
			m.err = nil
			m.status = "Manage targets: a add, e edit, t rotate token, d delete"
			return m, m.transition(screenManageTargets, cmds...)
		case "q":
			return m, tea.Quit
		}
	}
	return m, tea.Batch(cmds...)
}

func (m *model) updateJobs(msg tea.Msg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.jobs, cmd = m.jobs.Update(msg)
	cmds = append(cmds, cmd)
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "enter":
			selected := m.jobs.SelectedItem()
			item, ok := selected.(listItem)
			if !ok {
				return m, tea.Batch(cmds...)
			}
			if item.kind == models.JobNodeFolder {
				m.selectedJob = nil
				m.jobs.ResetFilter()
				m.jobFolders = append(m.jobFolders, models.JobNode{
					Name:     item.name,
					FullName: item.fullName,
					URL:      item.id,
					Kind:     models.JobNodeFolder,
				})
				return m, tea.Batch(append(cmds, m.loadCurrentFolderCmd(false))...)
			}
			if item.kind != models.JobNodeJob {
				return m, tea.Batch(cmds...)
			}
			job := models.JobRef{Name: item.name, FullName: item.fullName, URL: item.id}
			m.selectedJob = &job
			m.loading = true
			m.loadingStart = time.Now()
			m.loadingLabel = "Loading pipeline parameters"
			m.status = "Loading pipeline parameters..."
			return m, tea.Batch(append(cmds, loadParamsCmd(m.ctx, m.client, job.URL))...)
		case "esc":
			if m.jobs.SettingFilter() {
				return m, tea.Batch(cmds...)
			}
			return m.navigateUpJobs(cmds)
		case "backspace":
			if !m.jobs.SettingFilter() {
				return m.navigateUpJobs(cmds)
			}
		case "r":
			if m.jobs.SettingFilter() {
				return m, tea.Batch(cmds...)
			}
			return m, tea.Batch(append(cmds, m.loadCurrentFolderCmd(true))...)
		}
	}
	return m, tea.Batch(cmds...)
}

func (m *model) updateParams(msg tea.Msg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok && km.String() == "esc" {
		return m, m.transition(screenJobs, cmds...)
	}
	if m.paramForm == nil {
		return m, tea.Batch(cmds...)
	}
	updated, cmd := m.paramForm.Update(msg)
	if f, ok := updated.(*huh.Form); ok {
		m.paramForm = f
	}
	cmds = append(cmds, cmd)
	if m.paramForm.State == huh.StateCompleted {
		if err := m.buildPermutations(); err != nil {
			m.err = err
			m.status = "Invalid selections"
			m.buildParamForm()
			return m, tea.Batch(cmds...)
		}
		m.buildPreviewTable()
		m.status = fmt.Sprintf("%d permutations ready", len(m.permutations))
		return m, m.transition(screenPreview, cmds...)
	}
	return m, tea.Batch(cmds...)
}

func (m *model) updatePreview(msg tea.Msg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.previewTable, cmd = m.previewTable.Update(msg)
	cmds = append(cmds, cmd)
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "enter":
			m.startRun()
			return m, m.transition(screenRun, append(cmds, startRunCmd(m.runCtx, m.client, m.selectedJob.URL, m.permutations, concurrencyCap))...)
		case "esc", "backspace":
			m.buildParamForm()
			return m, m.transition(screenParams, cmds...)
		}
	}
	return m, tea.Batch(cmds...)
}

func (m *model) updateRun(msg tea.Msg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.runTable, cmd = m.runTable.Update(msg)
	cmds = append(cmds, cmd)
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "o":
			idx := m.runTable.Cursor()
			if idx >= 0 && idx < len(m.runRecords) {
				url := m.runRecords[idx].BuildURL
				if url != "" {
					_ = browser.Open(url)
				}
			}
		case "q":
			return m, tea.Quit
		case "r":
			if m.screen == screenDone {
				m.rebuildFailedOnly()
				m.buildPreviewTable()
				return m, m.transition(screenPreview, cmds...)
			}
		}
	}
	return m, tea.Batch(cmds...)
}

func (m *model) updateManageTargets(msg tea.Msg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.manage, cmd = m.manage.Update(msg)
	cmds = append(cmds, cmd)
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, tea.Batch(cmds...)
	}
	switch km.String() {
	case "esc":
		if m.manage.SettingFilter() {
			return m, tea.Batch(cmds...)
		}
		m.refreshServerItems()
		return m, m.transition(screenServers, cmds...)
	case "backspace":
		if m.manage.SettingFilter() {
			return m, tea.Batch(cmds...)
		}
		m.refreshServerItems()
		return m, m.transition(screenServers, cmds...)
	case "a":
		m.startManageForm(manageModeAdd, -1)
		return m, m.transition(screenManageForm, cmds...)
	case "enter", "e":
		idx := m.selectedManageTargetIndex()
		if idx < 0 {
			return m, tea.Batch(cmds...)
		}
		m.startManageForm(manageModeEdit, idx)
		return m, m.transition(screenManageForm, cmds...)
	case "t":
		idx := m.selectedManageTargetIndex()
		if idx < 0 {
			return m, tea.Batch(cmds...)
		}
		m.startManageForm(manageModeRotate, idx)
		return m, m.transition(screenManageForm, cmds...)
	case "d":
		idx := m.selectedManageTargetIndex()
		if idx < 0 {
			return m, tea.Batch(cmds...)
		}
		if err := m.deleteTargetAt(idx); err != nil {
			m.err = err
			m.status = "Failed to delete target"
		} else {
			m.err = nil
			m.status = "Deleted target"
		}
		return m, tea.Batch(cmds...)
	}
	return m, tea.Batch(cmds...)
}

func (m *model) updateManageForm(msg tea.Msg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		if km.String() == "esc" {
			m.manageForm = nil
			return m, m.transition(screenManageTargets, cmds...)
		}
	}
	if m.manageForm == nil {
		return m, m.transition(screenManageTargets, cmds...)
	}
	updated, cmd := m.manageForm.Update(msg)
	if f, ok := updated.(*huh.Form); ok {
		m.manageForm = f
	}
	cmds = append(cmds, cmd)
	if m.manageForm.State != huh.StateCompleted {
		return m, tea.Batch(cmds...)
	}
	if err := m.applyManageForm(); err != nil {
		m.err = err
		m.status = "Failed to save target"
		m.startManageForm(m.manageMode, m.manageIndex)
		return m, tea.Batch(cmds...)
	}
	m.manageForm = nil
	m.err = nil
	m.refreshManageItems()
	m.refreshServerItems()
	return m, m.transition(screenManageTargets, cmds...)
}

func (m *model) refreshServerItems() {
	items := make([]list.Item, 0, len(m.cfg.Jenkins))
	for idx, j := range m.cfg.Jenkins {
		desc := strings.TrimSpace(fmt.Sprintf("%s | %s", j.Username, j.Host))
		items = append(items, listItem{
			title: fmt.Sprintf("%d. %s", idx+1, j.Name),
			desc:  desc,
			id:    j.ID,
		})
	}
	m.servers.SetItems(items)
}

func (m *model) refreshManageItems() {
	items := make([]list.Item, 0, len(m.cfg.Jenkins))
	for _, j := range m.cfg.Jenkins {
		items = append(items, listItem{
			title: j.Name,
			desc:  fmt.Sprintf("%s (%s) [%s:%s]", j.Host, j.Username, j.Credential.Type, j.Credential.Ref),
			id:    j.ID,
		})
	}
	m.manage.SetItems(items)
}

func (m *model) findTargetByID(id string) *models.JenkinsTarget {
	for i := range m.cfg.Jenkins {
		if m.cfg.Jenkins[i].ID == id {
			return &m.cfg.Jenkins[i]
		}
	}
	return nil
}

func (m *model) selectedManageTargetIndex() int {
	selected := m.manage.SelectedItem()
	item, ok := selected.(listItem)
	if !ok {
		return -1
	}
	for i := range m.cfg.Jenkins {
		if m.cfg.Jenkins[i].ID == item.id {
			return i
		}
	}
	return -1
}

func (m *model) startManageForm(mode manageMode, idx int) {
	m.manageMode = mode
	m.manageIndex = idx
	m.manageID = ""
	m.manageHost = ""
	m.manageUsername = ""
	m.manageInsecure = "false"
	m.manageCredType = string(models.CredentialTypeKeyring)
	m.manageCredRef = ""
	m.manageToken = ""

	if idx >= 0 && idx < len(m.cfg.Jenkins) {
		t := m.cfg.Jenkins[idx]
		m.manageID = t.ID
		m.manageHost = t.Host
		m.manageUsername = t.Username
		if t.InsecureSkipTLSVerify {
			m.manageInsecure = "true"
		}
		m.manageCredType = string(t.Credential.Type)
		m.manageCredRef = t.Credential.Ref
	}

	fields := make([]huh.Field, 0)
	if mode == manageModeRotate {
		fields = append(fields,
			huh.NewNote().Title("Rotate Token").Description("Enter a new token for this target's keyring reference."),
			huh.NewInput().Title("Token").Value(&m.manageToken),
		)
	} else {
		fields = append(fields,
			huh.NewInput().Title("ID").Description("Unique target ID").Value(&m.manageID),
			huh.NewInput().Title("Host").Description("Jenkins base URL").Value(&m.manageHost),
			huh.NewInput().Title("Username").Value(&m.manageUsername),
			huh.NewSelect[string]().Title("Insecure TLS").Options(
				huh.NewOption("false", "false"),
				huh.NewOption("true", "true"),
			).Value(&m.manageInsecure),
			huh.NewSelect[string]().Title("Credential Type").Options(
				huh.NewOption(string(models.CredentialTypeKeyring), string(models.CredentialTypeKeyring)),
				huh.NewOption(string(models.CredentialTypeEnv), string(models.CredentialTypeEnv)),
			).Value(&m.manageCredType),
			huh.NewInput().Title("Credential Ref").Description("Keyring item name or environment variable").Value(&m.manageCredRef),
			huh.NewInput().Title("Token").Description("Required when adding keyring targets or rotating keyring refs").Value(&m.manageToken),
		)
	}
	m.manageForm = huh.NewForm(huh.NewGroup(fields...)).WithWidth(max(60, m.width-8))
}

func (m *model) applyManageForm() error {
	var previous *models.JenkinsTarget
	if m.manageMode == manageModeEdit {
		if m.manageIndex < 0 || m.manageIndex >= len(m.cfg.Jenkins) {
			return fmt.Errorf("invalid target selection")
		}
		prev := m.cfg.Jenkins[m.manageIndex]
		previous = &prev
	}

	switch m.manageMode {
	case manageModeRotate:
		if m.manageIndex < 0 || m.manageIndex >= len(m.cfg.Jenkins) {
			return fmt.Errorf("invalid target selection")
		}
		target := m.cfg.Jenkins[m.manageIndex]
		if target.Credential.Type != models.CredentialTypeKeyring {
			return fmt.Errorf("token rotation is only available for keyring-backed credentials")
		}
		token := strings.TrimSpace(m.manageToken)
		if token == "" {
			return fmt.Errorf("token is required")
		}
		if err := m.creds.SetKeyring(target.Credential.Ref, token); err != nil {
			return fmt.Errorf("store keyring token: %w", err)
		}
		m.status = "Token rotated"
		return nil
	}

	target, err := m.buildTargetFromForm(previous)
	if err != nil {
		return err
	}

	if m.manageMode == manageModeEdit {
		for i := range m.cfg.Jenkins {
			if i != m.manageIndex && m.cfg.Jenkins[i].ID == target.ID {
				return fmt.Errorf("target ID %q already exists", target.ID)
			}
		}
	}
	if m.manageMode == manageModeAdd {
		if m.findTargetByID(target.ID) != nil {
			return fmt.Errorf("target ID %q already exists", target.ID)
		}
	}

	if target.Credential.Type == models.CredentialTypeKeyring {
		token := strings.TrimSpace(m.manageToken)
		switch {
		case m.manageMode == manageModeAdd && token == "":
			return fmt.Errorf("token is required for keyring credentials")
		case m.manageMode == manageModeEdit && token == "" &&
			(previous == nil || previous.Credential.Type != models.CredentialTypeKeyring || previous.Credential.Ref != target.Credential.Ref):
			return fmt.Errorf("token is required when changing keyring credential ref")
		case token != "":
			if err := m.creds.SetKeyring(target.Credential.Ref, token); err != nil {
				return fmt.Errorf("store keyring token: %w", err)
			}
		}
	}

	previousTargets := append([]models.JenkinsTarget(nil), m.cfg.Jenkins...)
	var deleteOldKeyringRef string
	if m.manageMode == manageModeAdd {
		m.cfg.Jenkins = append(m.cfg.Jenkins, target)
	} else {
		m.cfg.Jenkins[m.manageIndex] = target
		if previous != nil && previous.Credential.Type == models.CredentialTypeKeyring &&
			(previous.Credential.Ref != target.Credential.Ref || target.Credential.Type != models.CredentialTypeKeyring) {
			deleteOldKeyringRef = previous.Credential.Ref
		}
	}

	if err := m.persistConfig(); err != nil {
		m.cfg.Jenkins = previousTargets
		return err
	}
	if deleteOldKeyringRef != "" {
		_ = m.creds.DeleteKeyring(deleteOldKeyringRef)
	}
	if m.manageMode == manageModeAdd {
		m.status = "Added target"
	} else {
		m.status = "Updated target"
	}
	return nil
}

func (m *model) buildTargetFromForm(previous *models.JenkinsTarget) (models.JenkinsTarget, error) {
	id := strings.TrimSpace(m.manageID)
	host := strings.TrimRight(strings.TrimSpace(m.manageHost), "/")
	username := strings.TrimSpace(m.manageUsername)
	credType := models.CredentialType(strings.TrimSpace(m.manageCredType))
	credRef := strings.TrimSpace(m.manageCredRef)
	switch {
	case id == "":
		return models.JenkinsTarget{}, fmt.Errorf("id is required")
	case host == "":
		return models.JenkinsTarget{}, fmt.Errorf("host is required")
	case username == "":
		return models.JenkinsTarget{}, fmt.Errorf("username is required")
	case credRef == "":
		return models.JenkinsTarget{}, fmt.Errorf("credential ref is required")
	case credType != models.CredentialTypeKeyring && credType != models.CredentialTypeEnv:
		return models.JenkinsTarget{}, fmt.Errorf("credential type must be %q or %q", models.CredentialTypeKeyring, models.CredentialTypeEnv)
	}

	name := id
	if previous != nil {
		prevName := strings.TrimSpace(previous.Name)
		prevID := strings.TrimSpace(previous.ID)
		if prevName != "" && prevName != prevID {
			name = prevName
		}
	}
	return models.JenkinsTarget{
		ID:       id,
		Name:     name,
		Host:     host,
		Username: username,
		Credential: models.Credential{
			Type: credType,
			Ref:  credRef,
		},
		InsecureSkipTLSVerify: m.manageInsecure == "true",
	}, nil
}

func (m *model) deleteTargetAt(idx int) error {
	if idx < 0 || idx >= len(m.cfg.Jenkins) {
		return fmt.Errorf("invalid target selection")
	}
	target := m.cfg.Jenkins[idx]
	if target.Credential.Type == models.CredentialTypeKeyring {
		_ = m.creds.DeleteKeyring(target.Credential.Ref)
	}
	next := make([]models.JenkinsTarget, 0, len(m.cfg.Jenkins)-1)
	next = append(next, m.cfg.Jenkins[:idx]...)
	next = append(next, m.cfg.Jenkins[idx+1:]...)
	m.cfg.Jenkins = next
	if m.target != nil && m.target.ID == target.ID {
		m.target = nil
		m.client = nil
		m.jobFolders = nil
		m.selectedJob = nil
	}
	if err := m.persistConfig(); err != nil {
		return err
	}
	m.refreshManageItems()
	m.refreshServerItems()
	return nil
}

func (m *model) persistConfig() error {
	if strings.TrimSpace(m.cfg.ConfigPath) == "" {
		return fmt.Errorf("config path is not set")
	}
	return config.Save(m.cfg.ConfigPath, m.cfg)
}

func (m *model) View() string {
	body := ""
	switch m.screen {
	case screenServers:
		body = m.servers.View()
	case screenManageTargets:
		body = m.manage.View()
	case screenManageForm:
		if m.manageForm != nil {
			body = m.manageForm.View()
		} else {
			body = "No form loaded"
		}
	case screenJobs:
		body = ui.Muted.Render("Path: "+jobsPathLabel(m.currentJobsPrefix())+" | enter: open folder/job | r: refresh current folder | /: filter") + "\n\n" + m.jobs.View()
	case screenParams:
		if m.paramForm != nil {
			body = m.paramForm.View()
		} else {
			body = "No parameters detected"
		}
	case screenPreview:
		body = m.previewTable.View()
	case screenRun, screenDone:
		body = m.runTable.View()
	}

	title := "jenkins-tui - Jenkins Permutation Runner"
	if m.target != nil {
		title += " | " + m.target.Name
	}
	if m.selectedJob != nil {
		title += " | " + m.selectedJob.FullName
	}

	help := helpTextForScreen(m.screen, m.screen == screenDone)
	status := m.status
	if status == "" {
		status = "Ready"
	}
	if m.screen == screenRun {
		status = m.spin.View() + " Tracking in progress"
	} else if m.loading {
		elapsed := time.Since(m.loadingStart).Round(time.Second)
		if elapsed < 0 {
			elapsed = 0
		}
		status = fmt.Sprintf("%s %s (elapsed: %s)", m.spin.View(), m.loadingLabel, elapsed)
	}

	errorLine := ""
	if m.err != nil {
		errorLine = ui.Danger.Render("Error: " + m.err.Error())
	}

	// Keep footer anchored to bottom by clipping only the middle body area.
	frameWidth := max(1, m.width)
	innerHeight := max(1, m.height)
	headerLines := []string{
		fitLineToWidth(ui.Title.Render(title), frameWidth),
		"",
	}
	footerLines := []string{
		fitLineToWidth(ui.Muted.Render(status), frameWidth),
		fitLineToWidth(ui.Help.Render(help), frameWidth),
	}
	if errorLine != "" {
		footerLines = append(footerLines, fitLineToWidth(errorLine, frameWidth))
	}

	headerHeight := len(headerLines)
	footerHeight := len(footerLines)
	bodyHeight := innerHeight - headerHeight - footerHeight
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	body = fitToBox(body, frameWidth, bodyHeight)

	content := strings.Join(append(append(headerLines, body), footerLines...), "\n")
	content = fitToBox(content, frameWidth, innerHeight)
	return ui.AppBorder.Width(frameWidth).Render(content)
}

func (m *model) buildParamForm() {
	m.choiceVars = map[string]*[]string{}
	m.fixedVars = map[string]*string{}
	fields := make([]huh.Field, 0, len(m.params))
	for _, p := range m.params {
		desc := p.Description
		if desc == "" {
			desc = string(p.Kind)
		}
		switch p.Kind {
		case models.ParamChoice:
			vals := []string{}
			opts := make([]huh.Option[string], 0, len(p.Choices))
			for _, ch := range p.Choices {
				opts = append(opts, huh.NewOption(ch, ch))
			}
			m.choiceVars[p.Name] = &vals
			fields = append(fields,
				huh.NewMultiSelect[string]().Title(p.Name).Description(desc).Options(opts...).Value(&vals),
			)
		default:
			v := p.Default
			m.fixedVars[p.Name] = &v
			if p.Kind == models.ParamBoolean {
				fields = append(fields,
					huh.NewSelect[string]().Title(p.Name).Description(desc).Options(
						huh.NewOption("true", "true"),
						huh.NewOption("false", "false"),
					).Value(&v),
				)
				continue
			}
			fields = append(fields, huh.NewInput().Title(p.Name).Description(desc).Value(&v))
		}
	}
	if len(fields) == 0 {
		m.paramForm = huh.NewForm(huh.NewGroup(huh.NewNote().Title("No supported parameters").Description("This job has no supported parameter types.")))
		return
	}
	m.paramForm = huh.NewForm(huh.NewGroup(fields...)).WithWidth(max(60, m.width-8))
}

func (m *model) buildPermutations() error {
	input := permutation.Input{
		ChoiceValues: map[string][]string{},
		FixedValues:  map[string]string{},
	}
	for k, v := range m.fixedVars {
		if v != nil {
			input.FixedValues[k] = *v
		}
	}
	for k, v := range m.choiceVars {
		if v != nil {
			input.ChoiceValues[k] = *v
		}
	}
	specs, err := permutation.Build(input, maxPermutations)
	if err != nil {
		return err
	}
	m.permutations = specs
	return nil
}

func (m *model) buildPreviewTable() {
	cols := []table.Column{
		{Title: "#", Width: 4},
		{Title: "Parameters", Width: max(24, m.width-22)},
	}
	rows := make([]table.Row, 0, len(m.permutations))
	for i, spec := range m.permutations {
		rows = append(rows, table.Row{
			fmt.Sprintf("%d", i+1),
			clip(summarizeParams(spec.Params), max(20, m.width-28)),
		})
	}
	t := table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(false),
		table.WithHeight(max(5, m.height-14)),
	)
	t.SetStyles(defaultTableStyles(false))
	m.previewTable = t
}

func (m *model) startRun() {
	m.runRecords = make([]models.RunRecord, 0, len(m.permutations))
	for i, spec := range m.permutations {
		m.runRecords = append(m.runRecords, models.RunRecord{Index: i, Spec: spec, State: models.RunPlanned, StartedAt: time.Now()})
	}
	m.finished = map[int]bool{}
	m.refreshRunTable()
	if m.runCancel != nil {
		m.runCancel()
	}
	runCtx, cancel := context.WithCancel(m.ctx)
	m.runCtx = runCtx
	m.runCancel = cancel
}

func (m *model) rebuildFailedOnly() {
	failed := make([]models.JobSpec, 0)
	for _, r := range m.runRecords {
		if r.State == models.RunFailed || r.State == models.RunAborted || r.State == models.RunError {
			failed = append(failed, r.Spec)
		}
	}
	if len(failed) > 0 {
		m.permutations = failed
		m.status = fmt.Sprintf("Prepared %d failed runs for retry", len(failed))
	}
}

func (m *model) applyRunUpdate(u models.RunUpdate) {
	if u.Index < 0 || u.Index >= len(m.runRecords) {
		return
	}
	r := m.runRecords[u.Index]
	r.State = u.State
	if u.QueueURL != "" {
		r.QueueURL = u.QueueURL
	}
	if u.BuildURL != "" {
		r.BuildURL = u.BuildURL
	}
	if u.BuildNumber != 0 {
		r.BuildNumber = u.BuildNumber
	}
	if u.Result != "" {
		r.Result = u.Result
	}
	if u.Err != nil {
		r.Err = u.Err.Error()
	}
	if u.Done {
		r.EndedAt = time.Now()
	}
	m.runRecords[u.Index] = r
}

func (m *model) refreshRunTable() {
	cursor := m.runTable.Cursor()
	cols := []table.Column{
		{Title: "#", Width: 4},
		{Title: "State", Width: 10},
		{Title: "Result", Width: 24},
		{Title: "Build URL", Width: max(20, m.width-50)},
	}
	rows := make([]table.Row, 0, len(m.runRecords))
	for _, r := range m.runRecords {
		result := r.Result
		if r.Err != "" {
			result = r.Err
		}
		url := r.BuildURL
		if url == "" {
			url = r.QueueURL
		}
		rows = append(rows, table.Row{
			fmt.Sprintf("%d", r.Index+1),
			string(r.State),
			clip(result, 24),
			clip(url, max(20, m.width-56)),
		})
	}
	t := table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(max(5, m.height-14)),
	)
	t.SetStyles(defaultTableStyles(true))
	m.runTable = t
	if cursor >= 0 && cursor < len(rows) {
		m.runTable.SetCursor(cursor)
	}
}

func summarizeParams(mv map[string]string) string {
	keys := make([]string, 0, len(mv))
	for k := range mv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, mv[k]))
	}
	return strings.Join(parts, ", ")
}

func (m *model) navigateUpJobs(cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	if len(m.jobFolders) == 0 {
		return m, m.transition(screenServers, cmds...)
	}
	m.jobs.ResetFilter()
	m.jobFolders = m.jobFolders[:len(m.jobFolders)-1]
	return m, tea.Batch(append(cmds, m.loadCurrentFolderCmd(false))...)
}

func (m *model) transition(next screen, cmds ...tea.Cmd) tea.Cmd {
	if m.screen != next {
		m.screen = next
		cmds = append(cmds, tea.ClearScreen)
	}
	return tea.Batch(cmds...)
}

func (m *model) loadCurrentFolderCmd(forceRefresh bool) tea.Cmd {
	if m.client == nil {
		return nil
	}
	containerURL, prefix := m.currentJobsContainer()
	m.jobsReqID++
	reqID := m.jobsReqID
	m.loading = true
	m.loadingStart = time.Now()
	action := "Loading"
	if forceRefresh {
		action = "Refreshing"
	}
	m.loadingLabel = fmt.Sprintf("%s %s", action, jobsPathLabel(prefix))
	m.status = m.loadingLabel + "..."
	return loadJobsCmd(m.ctx, m.cfg.CacheDir, m.client, containerURL, prefix, forceRefresh, reqID)
}

func (m *model) currentJobsContainer() (string, string) {
	if m.client == nil {
		return "", ""
	}
	if len(m.jobFolders) == 0 {
		return m.client.Host(), ""
	}
	last := m.jobFolders[len(m.jobFolders)-1]
	return last.URL, last.FullName
}

func (m *model) currentJobsPrefix() string {
	_, prefix := m.currentJobsContainer()
	return prefix
}

func loadJobsCmd(ctx context.Context, cacheDir string, client *jenkins.Client, containerURL, prefix string, forceRefresh bool, requestID uint64) tea.Cmd {
	return func() tea.Msg {
		if !forceRefresh {
			if nodes, ok, err := cache.JobNodesInDir(cacheDir, client.CacheKey(), containerURL); err == nil && ok {
				return jobsLoadedMsg{
					nodes:        nodes,
					fromCache:    true,
					requestID:    requestID,
					containerURL: containerURL,
					prefix:       prefix,
				}
			}
		}
		nodes, err := client.ListJobNodes(ctx, containerURL, prefix)
		if err != nil {
			return jobsLoadedMsg{
				nodes:        nodes,
				err:          err,
				requestID:    requestID,
				containerURL: containerURL,
				prefix:       prefix,
			}
		}
		_ = cache.SaveJobNodesInDir(cacheDir, client.CacheKey(), containerURL, nodes)
		return jobsLoadedMsg{
			nodes:        nodes,
			fromCache:    false,
			requestID:    requestID,
			containerURL: containerURL,
			prefix:       prefix,
		}
	}
}

func loadParamsCmd(ctx context.Context, client *jenkins.Client, jobURL string) tea.Cmd {
	return func() tea.Msg {
		params, err := client.GetJobParams(ctx, jobURL)
		return paramsLoadedMsg{params: params, err: err}
	}
}

func startRunCmd(ctx context.Context, client *jenkins.Client, jobURL string, specs []models.JobSpec, concurrency int) tea.Cmd {
	return func() tea.Msg {
		ch := make(chan models.RunUpdate)
		go executor.Run(ctx, client, jobURL, specs, concurrency, ch)
		return runStreamStartedMsg{ch: ch}
	}
}

func waitRunEventCmd(ch <-chan models.RunUpdate) tea.Cmd {
	return func() tea.Msg {
		update, ok := <-ch
		if !ok {
			return runDoneMsg{}
		}
		return runEventMsg{update: update}
	}
}

func jobsPathLabel(prefix string) string {
	if strings.TrimSpace(prefix) == "" {
		return "/"
	}
	return "/" + strings.TrimLeft(prefix, "/")
}

func paramsStatusMessage() string {
	return "Fill parameters. Choice fields support multi-select; ctrl+a toggles select all/none."
}

func helpTextForScreen(current screen, runDone bool) string {
	switch current {
	case screenJobs:
		return "enter: open folder/job | esc/backspace: up | r: refresh folder | q: quit"
	case screenParams:
		return "space/x: toggle | ctrl+a: select all/none | /: filter | shift+tab: back | enter: continue | q: quit"
	case screenManageTargets:
		return "a: add | e/enter: edit | t: rotate token | d: delete | esc: back | q: quit"
	case screenManageForm:
		return "enter: submit form | esc: cancel | q: quit"
	case screenRun, screenDone:
		help := "o: open build url | q: quit"
		if runDone {
			help += " | r: rerun failed"
		}
		return help
	default:
		return "enter: select/continue | m: manage targets | esc: back | q: quit"
	}
}

func clip(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	if n <= 3 {
		return lipgloss.NewStyle().MaxWidth(n).Render(s)
	}
	return lipgloss.NewStyle().MaxWidth(n-3).Render(s) + "..."
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func defaultTableStyles(focused bool) table.Styles {
	styles := table.DefaultStyles()
	styles.Header = styles.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		Bold(true).
		Foreground(lipgloss.Color("252"))
	if focused {
		styles.Selected = styles.Selected.
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("63")).
			Bold(true)
	} else {
		styles.Selected = styles.Selected.
			Foreground(lipgloss.Color("250")).
			Background(lipgloss.Color("236"))
	}
	return styles
}

func fitToBox(s string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	if len(lines) < height {
		lines = append(lines, make([]string, height-len(lines))...)
	}
	for i, line := range lines {
		lines[i] = fitLineToWidth(line, width)
	}
	return strings.Join(lines, "\n")
}

func fitLineToWidth(line string, width int) string {
	if width <= 0 {
		return ""
	}
	clipped := lipgloss.NewStyle().MaxWidth(width).Render(line)
	padding := width - lipgloss.Width(clipped)
	if padding <= 0 {
		return clipped
	}
	return clipped + strings.Repeat(" ", padding)
}

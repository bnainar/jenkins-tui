package tui

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"

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
	screenGlobalSearch
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

const (
	outerPaddingX = 2
	outerPaddingY = 1
)

const (
	tokenStorageKeyring = string(models.CredentialTypeKeyring)
	tokenStorageEnv     = string(models.CredentialTypeEnv)
)

type credentialsManager interface {
	Resolve(target models.JenkinsTarget) (string, error)
	SetKeyring(ref, value string) error
	DeleteKeyring(ref string) error
	KeyringAvailable() (bool, error)
}

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

type searchLoadedMsg struct {
	nodes     []models.JobNode
	err       error
	requestID uint64
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
	creds credentialsManager

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
	search  list.Model

	target      *models.JenkinsTarget
	client      *jenkins.Client
	selectedJob *models.JobRef
	jobFolders  []models.JobNode
	jobsReqID   uint64
	searchReqID uint64
	searchQuery string
	searchInput string

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
	manageName     string
	manageID       string
	manageIDManual string
	manageHost     string
	manageUsername string
	manageTokenSrc string
	manageInsecure string
	manageToken    string
	manageEnvVar   string
	manageKeyRef   string
	manageAdvanced bool
	keyringAvail   bool
	validateTarget func(ctx context.Context, target models.JenkinsTarget, token string, timeout time.Duration) error
	lookupEnv      func(key string) string
	helpExpanded   bool
	paramsBackTo   screen

	spin spinner.Model
}

func NewModel(ctx context.Context, cfg models.Config) tea.Model {
	serversDelegate := list.NewDefaultDelegate()
	applySelectedStyles(&serversDelegate)
	servers := list.New(nil, serversDelegate, 0, 0)
	servers.Title = "Jenkins Servers"
	servers.SetFilteringEnabled(true)
	servers.SetShowHelp(false)
	servers.SetShowStatusBar(false)
	servers.SetShowPagination(false)
	servers.DisableQuitKeybindings()

	jobsDelegate := list.NewDefaultDelegate()
	applySelectedStyles(&jobsDelegate)
	jobs := list.New(nil, jobsDelegate, 0, 0)
	jobs.Title = "Browse Jenkins Jobs"
	jobs.SetFilteringEnabled(true)
	jobs.SetShowHelp(false)
	jobs.SetShowStatusBar(false)
	jobs.SetShowPagination(false)
	jobs.DisableQuitKeybindings()

	manageDelegate := list.NewDefaultDelegate()
	applySelectedStyles(&manageDelegate)
	manage := list.New(nil, manageDelegate, 0, 0)
	manage.Title = "Manage Jenkins Servers"
	manage.SetFilteringEnabled(true)
	manage.SetShowHelp(false)
	manage.SetShowStatusBar(false)
	manage.SetShowPagination(false)
	manage.DisableQuitKeybindings()

	searchDelegate := list.NewDefaultDelegate()
	applySelectedStyles(&searchDelegate)
	search := list.New(nil, searchDelegate, 0, 0)
	search.Title = "Global Job Search"
	search.SetFilteringEnabled(false)
	search.SetShowHelp(false)
	search.SetShowStatusBar(false)
	search.SetShowPagination(false)
	search.DisableQuitKeybindings()

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
		search:         search,
		choiceVars:     map[string]*[]string{},
		fixedVars:      map[string]*string{},
		finished:       map[int]bool{},
		spin:           spin,
		manageInsecure: "false",
		manageTokenSrc: tokenStorageKeyring,
		manageIndex:    -1,
		lookupEnv:      os.Getenv,
		validateTarget: defaultTargetValidator,
		paramsBackTo:   screenJobs,
	}
	m.refreshServerItems()
	m.refreshManageItems()
	if len(cfg.Jenkins) == 0 {
		m.status = "No Jenkins servers configured. Add your first server."
		m.startManageForm(manageModeAdd, -1)
		m.screen = screenManageForm
	}
	return m
}

func (m *model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spin.Tick}
	if m.manageForm != nil {
		cmds = append(cmds, m.manageForm.Init())
	}
	if m.paramForm != nil {
		cmds = append(cmds, m.paramForm.Init())
	}
	return tea.Batch(cmds...)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.spin, cmd = m.spin.Update(msg)
	cmds := []tea.Cmd{cmd}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		contentWidth := m.contentWidth()
		contentHeight := m.contentHeight()
		m.servers.SetSize(max(0, contentWidth-8), max(0, contentHeight-10))
		m.jobs.SetSize(max(0, contentWidth-8), max(0, contentHeight-10))
		m.manage.SetSize(max(0, contentWidth-8), max(0, contentHeight-10))
		m.search.SetSize(max(0, contentWidth-8), max(0, contentHeight-10))
		if m.paramForm != nil {
			m.paramForm.WithWidth(max(1, contentWidth-8))
		}
		if m.manageForm != nil {
			m.manageForm.WithWidth(max(1, contentWidth-8))
		}
		if len(m.permutations) > 0 {
			m.buildPreviewTable()
		}
		if len(m.runRecords) > 0 {
			m.refreshRunTable()
		}
		m.previewTable.SetHeight(max(5, contentHeight-14))
		m.runTable.SetHeight(max(5, contentHeight-14))
		cmds = append(cmds, tea.ClearScreen)
	case tea.KeyMsg:
		if msg.String() == "?" {
			m.helpExpanded = !m.helpExpanded
		}
		if msg.String() == "ctrl+c" {
			if m.runCancel != nil {
				m.runCancel()
			}
			return m, tea.Quit
		}
		if msg.String() == "q" && m.allowQuickQuit() {
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
	case searchLoadedMsg:
		if typed.requestID != m.searchReqID {
			return m, tea.Batch(cmds...)
		}
		m.loading = false
		if typed.err != nil {
			m.err = typed.err
			m.status = "Global search failed"
			return m, tea.Batch(cmds...)
		}
		m.err = nil
		m.status = fmt.Sprintf("Found %d job(s)", len(typed.nodes))
		items := make([]list.Item, 0, len(typed.nodes))
		for _, n := range typed.nodes {
			items = append(items, listItem{
				title:    n.Name,
				desc:     n.FullName,
				id:       n.URL,
				name:     n.Name,
				fullName: n.FullName,
				kind:     n.Kind,
			})
		}
		m.search.SetItems(items)
		return m, tea.Batch(cmds...)
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
	case screenGlobalSearch:
		return m.updateGlobalSearch(msg, cmds)
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
				m.status = "Failed to resolve server credentials"
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
		case "a", "m":
			if m.servers.SettingFilter() {
				return m, tea.Batch(cmds...)
			}
			m.startManageForm(manageModeAdd, -1)
			m.err = nil
			return m, m.transition(screenManageForm, append(cmds, m.manageForm.Init())...)
		case "e":
			if m.servers.SettingFilter() {
				return m, tea.Batch(cmds...)
			}
			idx := m.selectedServerTargetIndex()
			if idx < 0 {
				return m, tea.Batch(cmds...)
			}
			m.startManageForm(manageModeEdit, idx)
			m.err = nil
			return m, m.transition(screenManageForm, append(cmds, m.manageForm.Init())...)
		case "t":
			if m.servers.SettingFilter() {
				return m, tea.Batch(cmds...)
			}
			idx := m.selectedServerTargetIndex()
			if idx < 0 {
				return m, tea.Batch(cmds...)
			}
			m.startManageForm(manageModeRotate, idx)
			m.err = nil
			return m, m.transition(screenManageForm, append(cmds, m.manageForm.Init())...)
		case "d":
			if m.servers.SettingFilter() {
				return m, tea.Batch(cmds...)
			}
			idx := m.selectedServerTargetIndex()
			if idx < 0 {
				return m, tea.Batch(cmds...)
			}
			if err := m.deleteTargetAt(idx); err != nil {
				m.err = err
				m.status = "Failed to delete server"
				return m, tea.Batch(cmds...)
			}
			m.err = nil
			if len(m.cfg.Jenkins) == 0 {
				m.status = "No Jenkins servers configured. Add your first server."
				m.startManageForm(manageModeAdd, -1)
				return m, m.transition(screenManageForm, append(cmds, m.manageForm.Init())...)
			}
			m.status = "Deleted server"
			return m, tea.Batch(cmds...)
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
			m.paramsBackTo = screenJobs
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
		case "g":
			if m.jobs.SettingFilter() {
				return m, tea.Batch(cmds...)
			}
			m.searchInput = ""
			m.searchQuery = ""
			m.search.SetItems(nil)
			m.search.Title = "Global Job Search"
			m.status = "Type to search jobs across this Jenkins server"
			return m, m.transition(screenGlobalSearch, cmds...)
		}
	}
	return m, tea.Batch(cmds...)
}

func (m *model) updateGlobalSearch(msg tea.Msg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	cmds = append(cmds, cmd)
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, tea.Batch(cmds...)
	}
	switch km.String() {
	case "esc":
		m.searchInput = ""
		m.searchQuery = ""
		m.search.SetItems(nil)
		return m, m.transition(screenJobs, cmds...)
	case "enter":
		selected := m.search.SelectedItem()
		item, ok := selected.(listItem)
		if !ok {
			return m, tea.Batch(cmds...)
		}
		job := models.JobRef{Name: item.name, FullName: item.fullName, URL: item.id}
		m.selectedJob = &job
		m.paramsBackTo = screenGlobalSearch
		m.loading = true
		m.loadingStart = time.Now()
		m.loadingLabel = "Loading pipeline parameters"
		m.status = "Loading pipeline parameters..."
		return m, tea.Batch(append(cmds, loadParamsCmd(m.ctx, m.client, job.URL))...)
	case "backspace":
		if m.searchInput != "" {
			m.searchInput = m.searchInput[:len(m.searchInput)-1]
		}
	case "r":
		if strings.TrimSpace(m.searchInput) == "" {
			return m, tea.Batch(cmds...)
		}
	default:
		if len(km.Runes) > 0 {
			m.searchInput += string(km.Runes)
		}
	}
	m.searchQuery = strings.TrimSpace(m.searchInput)
	m.search.Title = "Global Job Search: " + m.searchQuery
	if len(m.searchQuery) < 2 {
		m.search.SetItems(nil)
		m.status = "Type at least 2 characters"
		return m, tea.Batch(cmds...)
	}
	m.searchReqID++
	reqID := m.searchReqID
	m.loading = true
	m.loadingStart = time.Now()
	m.loadingLabel = "Searching jobs"
	m.status = "Searching jobs..."
	return m, tea.Batch(append(cmds, loadSearchCmd(m.ctx, m.client, m.searchQuery, reqID))...)
}

func (m *model) updateParams(msg tea.Msg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok && km.String() == "esc" {
		return m, m.transition(m.paramsBackTo, cmds...)
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
			return m, tea.Batch(append(cmds, m.paramForm.Init())...)
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
			return m, m.transition(screenParams, append(cmds, m.paramForm.Init())...)
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
		return m, m.transition(screenManageForm, append(cmds, m.manageForm.Init())...)
	case "enter", "e":
		idx := m.selectedManageTargetIndex()
		if idx < 0 {
			return m, tea.Batch(cmds...)
		}
		m.startManageForm(manageModeEdit, idx)
		return m, m.transition(screenManageForm, append(cmds, m.manageForm.Init())...)
	case "t":
		idx := m.selectedManageTargetIndex()
		if idx < 0 {
			return m, tea.Batch(cmds...)
		}
		m.startManageForm(manageModeRotate, idx)
		return m, m.transition(screenManageForm, append(cmds, m.manageForm.Init())...)
	case "d":
		idx := m.selectedManageTargetIndex()
		if idx < 0 {
			return m, tea.Batch(cmds...)
		}
		if err := m.deleteTargetAt(idx); err != nil {
			m.err = err
			m.status = "Failed to delete server"
		} else {
			m.err = nil
			m.status = "Deleted server"
		}
		return m, tea.Batch(cmds...)
	}
	return m, tea.Batch(cmds...)
}

func (m *model) updateManageForm(msg tea.Msg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		if km.String() == "esc" {
			m.manageForm = nil
			if len(m.cfg.Jenkins) == 0 {
				m.status = "No Jenkins servers configured. Press a to add one."
			}
			return m, m.transition(screenServers, cmds...)
		}
	}
	if m.manageForm == nil {
		return m, m.transition(screenServers, cmds...)
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
		m.status = "Failed to save server"
		m.startManageForm(m.manageMode, m.manageIndex)
		return m, tea.Batch(append(cmds, m.manageForm.Init())...)
	}
	m.manageForm = nil
	m.err = nil
	m.refreshManageItems()
	m.refreshServerItems()
	return m, m.transition(screenServers, cmds...)
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
		source := "system password manager"
		if j.Credential.Type == models.CredentialTypeEnv {
			source = "environment variable"
		}
		items = append(items, listItem{
			title: j.Name,
			desc:  fmt.Sprintf("%s (%s) [%s: %s]", j.Host, j.Username, source, j.Credential.Ref),
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

func (m *model) selectedServerTargetIndex() int {
	selected := m.servers.SelectedItem()
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
	m.manageName = ""
	m.manageID = ""
	m.manageIDManual = ""
	m.manageHost = ""
	m.manageUsername = ""
	m.manageTokenSrc = tokenStorageKeyring
	m.manageInsecure = "false"
	m.manageToken = ""
	m.manageEnvVar = ""
	m.manageKeyRef = ""
	m.manageAdvanced = false
	m.keyringAvail = true

	available, err := m.creds.KeyringAvailable()
	if err != nil || !available {
		m.keyringAvail = false
		m.manageTokenSrc = tokenStorageEnv
	}

	if idx >= 0 && idx < len(m.cfg.Jenkins) {
		t := m.cfg.Jenkins[idx]
		m.manageID = t.ID
		m.manageHost = t.Host
		m.manageUsername = t.Username
		m.manageName = t.Name
		if t.InsecureSkipTLSVerify {
			m.manageInsecure = "true"
			m.manageAdvanced = true
		}
		if t.Credential.Type == models.CredentialTypeEnv {
			m.manageTokenSrc = tokenStorageEnv
			m.manageEnvVar = t.Credential.Ref
		} else {
			defaultRef := defaultKeyringRef(t.ID)
			if t.Credential.Ref != "" && t.Credential.Ref != defaultRef {
				m.manageKeyRef = t.Credential.Ref
				m.manageAdvanced = true
			}
			if m.keyringAvail {
				m.manageTokenSrc = tokenStorageKeyring
			} else {
				m.manageTokenSrc = tokenStorageEnv
			}
		}
	}

	if mode == manageModeRotate {
		m.manageForm = huh.NewForm(huh.NewGroup(
			huh.NewNote().Title("Rotate API Token").Description("Enter a new token for this server's system password manager entry."),
			huh.NewInput().Title("API Token").Description("Stores a new token in the system password manager.").Password(true).Value(&m.manageToken),
		).Title("Rotate API Token")).WithTheme(ui.FormTheme()).WithWidth(max(60, m.contentWidth()-8))
		return
	}

	formTitle := "Add Jenkins Server"
	if mode == manageModeEdit {
		formTitle = "Edit Jenkins Server"
	}

	coreFields := make([]huh.Field, 0, 6)
	if !m.keyringAvail {
		coreFields = append(coreFields, huh.NewNote().
			Title("System password manager unavailable").
			Description("System password manager unavailable; using environment variable mode."))
	}
	coreFields = append(coreFields,
		huh.NewInput().
			Title("Jenkins URL").
			Description("Example: https://jenkins.example.com").
			Value(&m.manageHost),
		huh.NewInput().
			Title("Username").
			Description("Jenkins username used with API token auth").
			Value(&m.manageUsername),
		huh.NewInput().
			Title("Server Name").
			Description("How this Jenkins server appears in the list").
			Value(&m.manageName),
	)
	tokenOptions := []huh.Option[string]{}
	if m.keyringAvail {
		tokenOptions = append(tokenOptions, huh.NewOption("System password manager (recommended)", tokenStorageKeyring))
	}
	tokenOptions = append(tokenOptions, huh.NewOption("Environment variable", tokenStorageEnv))
	coreFields = append(coreFields,
		huh.NewSelect[string]().
			Title("Token Storage").
			Description("Where jenkins-tui should read your API token from.").
			Options(tokenOptions...).
			Value(&m.manageTokenSrc),
		huh.NewConfirm().
			Title("Advanced settings?").
			Affirmative("Yes").
			Negative("No").
			Value(&m.manageAdvanced),
	)

	coreGroup := huh.NewGroup(coreFields...).Title(formTitle)
	keyringTokenGroup := huh.NewGroup(
		huh.NewInput().
			Title("API Token").
			Description("Paste token; saved in your OS password manager").
			Password(true).
			Value(&m.manageToken),
	).WithHideFunc(func() bool {
		return !m.keyringAvail || m.manageTokenSrc != tokenStorageKeyring
	})
	envTokenGroup := huh.NewGroup(
		huh.NewInput().
			Title("Token Environment Variable").
			Description("Variable name, e.g. JENKINS_TOKEN_PROD").
			Value(&m.manageEnvVar),
	).WithHideFunc(func() bool {
		return m.manageTokenSrc != tokenStorageEnv
	})
	advancedGroup := huh.NewGroup(
		huh.NewInput().
			Title("Internal ID override").
			Description("Used in config; leave blank to auto-generate").
			Value(&m.manageIDManual),
		huh.NewSelect[string]().
			Title("Skip TLS certificate verification").
			Description("Only for trusted self-signed/internal certs").
			Options(
				huh.NewOption("false", "false"),
				huh.NewOption("true", "true"),
			).
			Value(&m.manageInsecure),
	).Title("Advanced").WithHideFunc(func() bool {
		return !m.manageAdvanced
	})
	keyringAdvancedGroup := huh.NewGroup(
		huh.NewInput().
			Title("Password manager entry override").
			Description("Default: jenkins-tui/<internal-id>").
			Value(&m.manageKeyRef),
	).Title("Advanced").WithHideFunc(func() bool {
		return !m.manageAdvanced || !m.keyringAvail || m.manageTokenSrc != tokenStorageKeyring
	})

	m.manageForm = huh.NewForm(coreGroup, keyringTokenGroup, envTokenGroup, advancedGroup, keyringAdvancedGroup).
		WithTheme(ui.FormTheme()).
		WithWidth(max(60, m.contentWidth()-8))
}

func (m *model) applyManageForm() error {
	var previous *models.JenkinsTarget
	if m.manageMode == manageModeEdit {
		if m.manageIndex < 0 || m.manageIndex >= len(m.cfg.Jenkins) {
			return fmt.Errorf("invalid server selection")
		}
		prev := m.cfg.Jenkins[m.manageIndex]
		previous = &prev
	}

	switch m.manageMode {
	case manageModeRotate:
		if m.manageIndex < 0 || m.manageIndex >= len(m.cfg.Jenkins) {
			return fmt.Errorf("invalid server selection")
		}
		target := m.cfg.Jenkins[m.manageIndex]
		if target.Credential.Type != models.CredentialTypeKeyring {
			return fmt.Errorf("token rotation is only available for keyring-backed credentials")
		}
		token := strings.TrimSpace(m.manageToken)
		if token == "" {
			return fmt.Errorf("API token is required.")
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
	tokenForValidation, typedKeyringToken, err := m.resolveTokenForValidation(target, previous)
	if err != nil {
		return err
	}
	validator := m.validateTarget
	if validator == nil {
		validator = defaultTargetValidator
	}
	if err := validator(m.ctx, target, tokenForValidation, m.cfg.Timeout); err != nil {
		return mapTargetValidationError(err)
	}

	if target.Credential.Type == models.CredentialTypeKeyring && typedKeyringToken != "" {
		if err := m.creds.SetKeyring(target.Credential.Ref, typedKeyringToken); err != nil {
			return fmt.Errorf("store API token in system password manager: %w", err)
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
		m.status = "Added server"
	} else {
		m.status = "Updated server"
	}
	return nil
}

func (m *model) buildTargetFromForm(previous *models.JenkinsTarget) (models.JenkinsTarget, error) {
	host := strings.TrimRight(strings.TrimSpace(m.manageHost), "/")
	name := strings.TrimSpace(m.manageName)
	if name == "" {
		name = deriveNameFromHost(host)
	}
	if name == "" {
		name = "Jenkins"
	}
	username := strings.TrimSpace(m.manageUsername)
	idManual := strings.TrimSpace(m.manageIDManual)
	id := ""
	switch {
	case host == "":
		return models.JenkinsTarget{}, fmt.Errorf("Jenkins URL is required.")
	case username == "":
		return models.JenkinsTarget{}, fmt.Errorf("Username is required.")
	}

	if previous != nil && idManual == "" {
		id = strings.TrimSpace(previous.ID)
	} else if idManual != "" {
		id = slugifyID(idManual)
		if m.idExists(id, previous) {
			return models.JenkinsTarget{}, fmt.Errorf("Internal ID %q already exists.", id)
		}
	} else {
		id = m.uniqueAutoID(slugifyID(name), previous)
	}

	credType := models.CredentialType(m.manageTokenSrc)
	credRef := ""
	switch credType {
	case models.CredentialTypeKeyring:
		if !m.keyringAvail {
			return models.JenkinsTarget{}, fmt.Errorf("System password manager is unavailable. Choose environment variable token storage.")
		}
		credRef = strings.TrimSpace(m.manageKeyRef)
		if credRef == "" {
			credRef = defaultKeyringRef(id)
		}
	case models.CredentialTypeEnv:
		credRef = strings.TrimSpace(m.manageEnvVar)
		if credRef == "" {
			return models.JenkinsTarget{}, fmt.Errorf("Token environment variable is required.")
		}
	default:
		return models.JenkinsTarget{}, fmt.Errorf("Token storage must be system password manager or environment variable.")
	}

	m.manageID = id
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

func (m *model) resolveTokenForValidation(target models.JenkinsTarget, previous *models.JenkinsTarget) (string, string, error) {
	switch target.Credential.Type {
	case models.CredentialTypeKeyring:
		typed := strings.TrimSpace(m.manageToken)
		if typed != "" {
			return typed, typed, nil
		}
		if previous != nil &&
			previous.Credential.Type == models.CredentialTypeKeyring &&
			previous.Credential.Ref == target.Credential.Ref {
			token, err := m.creds.Resolve(target)
			if err == nil && strings.TrimSpace(token) != "" {
				return token, "", nil
			}
		}
		return "", "", fmt.Errorf("API token is required.")
	case models.CredentialTypeEnv:
		lookup := m.lookupEnv
		if lookup == nil {
			lookup = os.Getenv
		}
		envVar := strings.TrimSpace(target.Credential.Ref)
		token := strings.TrimSpace(lookup(envVar))
		if token == "" {
			return "", "", fmt.Errorf("Environment variable %s is not set or empty.", envVar)
		}
		return token, "", nil
	default:
		return "", "", fmt.Errorf("unsupported credential type %q", target.Credential.Type)
	}
}

func (m *model) uniqueAutoID(base string, previous *models.JenkinsTarget) string {
	id := slugifyID(base)
	if id == "" {
		id = "target"
	}
	if !m.idExists(id, previous) {
		return id
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", id, i)
		if !m.idExists(candidate, previous) {
			return candidate
		}
	}
}

func (m *model) idExists(id string, previous *models.JenkinsTarget) bool {
	for i := range m.cfg.Jenkins {
		existing := m.cfg.Jenkins[i]
		if existing.ID != id {
			continue
		}
		if previous != nil && existing.ID == previous.ID {
			continue
		}
		return true
	}
	return false
}

func deriveNameFromHost(host string) string {
	raw := strings.TrimSpace(host)
	if raw == "" {
		return ""
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}
	if !strings.Contains(raw, "://") {
		if parsed, err := url.Parse("https://" + raw); err == nil && parsed.Hostname() != "" {
			return parsed.Hostname()
		}
	}
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.TrimPrefix(raw, "http://")
	if idx := strings.Index(raw, "/"); idx >= 0 {
		raw = raw[:idx]
	}
	if idx := strings.Index(raw, ":"); idx >= 0 {
		raw = raw[:idx]
	}
	return strings.TrimSpace(raw)
}

func slugifyID(input string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(input)) {
		isASCIIAlphaNum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isASCIIAlphaNum {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if unicode.IsSpace(r) || !isASCIIAlphaNum {
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "target"
	}
	return out
}

func defaultKeyringRef(id string) string {
	return "jenkins-tui/" + slugifyID(id)
}

func mapTargetValidationError(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "(401)") || strings.Contains(msg, "(403)"):
		return fmt.Errorf("Authentication failed. Check username and API token.")
	case strings.Contains(msg, "x509") || strings.Contains(msg, "tls") || strings.Contains(msg, "certificate"):
		return fmt.Errorf("TLS certificate verification failed. Enable 'Skip TLS certificate verification' only if you trust this server.")
	default:
		return fmt.Errorf("Failed to validate Jenkins server: %s", err.Error())
	}
}

func defaultTargetValidator(ctx context.Context, target models.JenkinsTarget, token string, timeout time.Duration) error {
	return jenkins.NewClient(target, token, timeout).ValidateConnection(ctx)
}

func (m *model) deleteTargetAt(idx int) error {
	if idx < 0 || idx >= len(m.cfg.Jenkins) {
		return fmt.Errorf("invalid server selection")
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
		body = ui.Muted.Render("Path: "+jobsPathLabel(m.currentJobsPrefix())) + "\n\n" + m.jobs.View()
	case screenGlobalSearch:
		body = ui.Muted.Render("Search: "+m.searchInput) + "\n\n" + m.search.View()
	case screenParams:
		if m.paramForm != nil {
			body = m.paramForm.View()
			if label := selectedJobLabel(m.selectedJob); label != "" {
				body = ui.Muted.Render("Job: "+label) + "\n\n" + body
			}
		} else {
			body = "No parameters detected"
		}
	case screenPreview:
		body = m.previewTable.View()
	case screenRun, screenDone:
		body = m.runTable.View()
	}

	help := helpTextForScreen(m.screen, m.screen == screenDone, m.helpExpanded)
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
	frameWidth := m.contentWidth()
	innerHeight := m.contentHeight()
	headerLines := []string{}
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
	padded := lipgloss.NewStyle().Padding(outerPaddingY, outerPaddingX).Render(content)
	return ui.AppBorder.Render(padded)
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
		m.paramForm = huh.NewForm(huh.NewGroup(huh.NewNote().Title("No supported parameters").Description("This job has no supported parameter types."))).WithTheme(ui.FormTheme())
		return
	}
	m.paramForm = huh.NewForm(huh.NewGroup(fields...)).WithTheme(ui.FormTheme()).WithWidth(max(60, m.contentWidth()-8))
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
	contentWidth := m.contentWidth()
	contentHeight := m.contentHeight()
	cols := []table.Column{
		{Title: "#", Width: 4},
		{Title: "Parameters", Width: max(24, contentWidth-22)},
	}
	rows := make([]table.Row, 0, len(m.permutations))
	for i, spec := range m.permutations {
		rows = append(rows, table.Row{
			fmt.Sprintf("%d", i+1),
			clip(summarizeParams(spec.Params), max(20, contentWidth-28)),
		})
	}
	t := table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(false),
		table.WithHeight(max(5, contentHeight-14)),
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
	contentWidth := m.contentWidth()
	contentHeight := m.contentHeight()
	cols := []table.Column{
		{Title: "#", Width: 4},
		{Title: "State", Width: 10},
		{Title: "Result", Width: 24},
		{Title: "Build URL", Width: max(20, contentWidth-50)},
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
			clip(url, max(20, contentWidth-56)),
		})
	}
	t := table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(max(5, contentHeight-14)),
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

func applySelectedStyles(delegate *list.DefaultDelegate) {
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		BorderForeground(lipgloss.Color("110")).
		Foreground(lipgloss.Color("252"))
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedTitle.Copy().Foreground(lipgloss.Color("245"))
}

func selectedJobLabel(job *models.JobRef) string {
	if job == nil {
		return ""
	}
	if strings.TrimSpace(job.FullName) != "" {
		return jobsPathLabel(job.FullName)
	}
	return strings.TrimSpace(job.Name)
}

func loadParamsCmd(ctx context.Context, client *jenkins.Client, jobURL string) tea.Cmd {
	return func() tea.Msg {
		params, err := client.GetJobParams(ctx, jobURL)
		return paramsLoadedMsg{params: params, err: err}
	}
}

func loadSearchCmd(ctx context.Context, client *jenkins.Client, query string, requestID uint64) tea.Cmd {
	return func() tea.Msg {
		nodes, err := client.SearchJobs(ctx, query, 100)
		return searchLoadedMsg{nodes: nodes, err: err, requestID: requestID}
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

func helpTextForScreen(current screen, runDone bool, expanded bool) string {
	if !expanded {
		switch current {
		case screenServers:
			return "enter select | a add | e edit | q quit | ? more"
		case screenJobs:
			return "enter open | / filter | g global search | q quit | ? more"
		case screenGlobalSearch:
			return "type search | enter open | esc back | ? more"
		case screenParams:
			return "enter continue | esc back | ? more"
		case screenRun, screenDone:
			return "o open url | q quit | ? more"
		default:
			return "q quit | ? more"
		}
	}
	switch current {
	case screenServers:
		return "enter: select server | a/m: add | e: edit | t: rotate token | d: delete | q: quit"
	case screenJobs:
		return "enter: open folder/job | esc/backspace: up | r: refresh folder | /: filter | g: global search | q: quit"
	case screenGlobalSearch:
		return "type: query | enter: open job | backspace: edit | r: refresh | esc: back | q: quit"
	case screenParams:
		return "space/x: toggle | ctrl+a: select all/none | /: filter | shift+tab: back | enter: continue | ctrl+c: quit"
	case screenManageTargets:
		return "a: add | e/enter: edit | t: rotate token | d: delete | esc: back | q: quit"
	case screenManageForm:
		return "enter: next/submit | shift+tab: back | esc: cancel | ctrl+c: quit"
	case screenRun, screenDone:
		help := "o: open build url | q: quit"
		if runDone {
			help += " | r: rerun failed"
		}
		return help
	default:
		return "enter: continue | q: quit"
	}
}

func (m *model) allowQuickQuit() bool {
	switch m.screen {
	case screenServers:
		return !m.servers.SettingFilter()
	case screenJobs:
		return !m.jobs.SettingFilter()
	case screenGlobalSearch:
		return true
	case screenManageTargets:
		return !m.manage.SettingFilter()
	case screenParams, screenManageForm:
		// Preserve typed "q" in form input contexts.
		return false
	default:
		return true
	}
}

func (m *model) contentWidth() int {
	return max(1, m.width-(outerPaddingX*2))
}

func (m *model) contentHeight() int {
	return max(1, m.height-(outerPaddingY*2))
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
		Foreground(lipgloss.Color("250"))
	if focused {
		styles.Selected = styles.Selected.
			Foreground(lipgloss.Color("252")).
			Background(lipgloss.Color("236")).
			Bold(true)
	} else {
		styles.Selected = styles.Selected.
			Foreground(lipgloss.Color("252")).
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

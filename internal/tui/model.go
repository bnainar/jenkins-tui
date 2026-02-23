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

	"jenx/internal/browser"
	"jenx/internal/cache"
	"jenx/internal/executor"
	"jenx/internal/jenkins"
	"jenx/internal/models"
	"jenx/internal/permutation"
	"jenx/internal/ui"
)

type screen int

const (
	screenServers screen = iota
	screenJobs
	screenParams
	screenPreview
	screenRun
	screenDone
)

const (
	maxPermutations = 20
	concurrencyCap  = 4
)

type listItem struct {
	title string
	desc  string
	id    string
}

func (i listItem) Title() string       { return i.title }
func (i listItem) Description() string { return i.desc }
func (i listItem) FilterValue() string { return i.title + " " + i.desc }

type jobsLoadedMsg struct {
	jobs      []models.JobRef
	fromCache bool
	err       error
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

type model struct {
	ctx context.Context
	cfg models.Config

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

	target      *models.JenkinsTarget
	client      *jenkins.Client
	selectedJob *models.JobRef

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

	spin spinner.Model
}

func NewModel(ctx context.Context, cfg models.Config) tea.Model {
	items := make([]list.Item, 0, len(cfg.Jenkins))
	for idx, j := range cfg.Jenkins {
		desc := j.Host
		if strings.TrimSpace(j.Name) != "" {
			desc = fmt.Sprintf("%s (%s)", j.Name, j.Host)
		}
		items = append(items, listItem{title: fmt.Sprintf("%d. %s", idx+1, j.Name), desc: desc, id: j.Host})
	}

	servers := list.New(items, list.NewDefaultDelegate(), 0, 0)
	servers.Title = "Select Jenkins"
	servers.SetFilteringEnabled(true)

	jobs := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	jobs.Title = "Select Parameterized Pipeline"
	jobs.SetFilteringEnabled(true)

	spin := spinner.New()
	spin.Spinner = spinner.Dot
	return &model{
		ctx:        ctx,
		cfg:        cfg,
		screen:     screenServers,
		servers:    servers,
		jobs:       jobs,
		choiceVars: map[string]*[]string{},
		fixedVars:  map[string]*string{},
		finished:   map[int]bool{},
		spin:       spin,
	}
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
		m.servers.SetSize(msg.Width-8, msg.Height-10)
		m.jobs.SetSize(msg.Width-8, msg.Height-10)
		m.previewTable.SetHeight(max(5, msg.Height-14))
		m.runTable.SetHeight(max(5, msg.Height-14))
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
		m.loading = false
		if typed.err != nil {
			m.err = typed.err
			m.status = "Failed to load jobs"
			return m, tea.Batch(cmds...)
		}
		m.err = nil
		if typed.fromCache {
			m.status = fmt.Sprintf("Loaded %d parameterized jobs (cache, TTL 24h)", len(typed.jobs))
		} else {
			m.status = fmt.Sprintf("Loaded %d parameterized jobs", len(typed.jobs))
		}
		items := make([]list.Item, 0, len(typed.jobs))
		for _, j := range typed.jobs {
			items = append(items, listItem{title: j.FullName, desc: j.URL, id: j.URL})
		}
		m.jobs.SetItems(items)
		m.screen = screenJobs
		return m, tea.Batch(append(cmds, tea.ClearScreen)...)
	case paramsLoadedMsg:
		m.loading = false
		if typed.err != nil {
			m.err = typed.err
			m.status = "Failed to load parameters"
			return m, tea.Batch(cmds...)
		}
		m.params = typed.params
		m.buildParamForm()
		m.screen = screenParams
		m.status = "Fill parameters. Choice fields support multi-select."
		return m, tea.Batch(append(cmds, tea.ClearScreen)...)
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
			m.screen = screenDone
			m.status = "All jobs finished"
			return m, tea.Batch(append(cmds, tea.ClearScreen)...)
		}
		return m, waitRunEventCmd(m.runEvents)
	case runDoneMsg:
		if m.screen == screenRun {
			m.screen = screenDone
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
			var t *models.JenkinsTarget
			for i := range m.cfg.Jenkins {
				if m.cfg.Jenkins[i].Host == item.id {
					t = &m.cfg.Jenkins[i]
					break
				}
			}
			if t == nil {
				return m, tea.Batch(cmds...)
			}
			m.target = t
			m.client = jenkins.NewClient(*t)
			m.loading = true
			m.loadingStart = time.Now()
			m.loadingLabel = "Loading jobs"
			m.status = "Loading jobs..."
			return m, tea.Batch(append(cmds, loadJobsCmd(m.ctx, m.client))...)
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
			job := models.JobRef{FullName: item.title, URL: item.id}
			m.selectedJob = &job
			m.loading = true
			m.loadingStart = time.Now()
			m.loadingLabel = "Loading pipeline parameters"
			m.status = "Loading pipeline parameters..."
			return m, tea.Batch(append(cmds, loadParamsCmd(m.ctx, m.client, job.URL))...)
		case "esc", "backspace":
			m.screen = screenServers
		}
	}
	return m, tea.Batch(cmds...)
}

func (m *model) updateParams(msg tea.Msg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok && km.String() == "esc" {
		m.screen = screenJobs
		return m, tea.Batch(cmds...)
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
		m.screen = screenPreview
		m.status = fmt.Sprintf("%d permutations ready", len(m.permutations))
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
			return m, tea.Batch(append(cmds, startRunCmd(m.runCtx, m.client, m.selectedJob.URL, m.permutations, concurrencyCap))...)
		case "esc", "backspace":
			m.buildParamForm()
			m.screen = screenParams
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
				m.screen = screenPreview
			}
		}
	}
	return m, tea.Batch(cmds...)
}

func (m *model) View() string {
	body := ""
	switch m.screen {
	case screenServers:
		body = m.servers.View()
	case screenJobs:
		body = ui.Muted.Render("Tip: press / to filter jobs, then Enter to apply filter.") + "\n\n" + m.jobs.View()
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

	title := "jenx - Jenkins Permutation Runner"
	if m.target != nil {
		title += " | " + m.target.Name
	}
	if m.selectedJob != nil {
		title += " | " + m.selectedJob.FullName
	}

	help := "enter: select/continue | esc: back | q: quit"
	if m.screen == screenRun || m.screen == screenDone {
		help = "o: open build url | q: quit"
		if m.screen == screenDone {
			help += " | r: rerun failed"
		}
	}
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
	frameWidth := max(40, m.width-2)
	innerHeight := max(8, m.height-4)
	headerLines := []string{
		ui.Title.Render(title),
		"",
	}
	footerLines := []string{
		ui.Muted.Render(status),
		ui.Help.Render(help),
	}
	if errorLine != "" {
		footerLines = append(footerLines, errorLine)
	}

	headerHeight := len(headerLines)
	footerHeight := len(footerLines)
	bodyHeight := innerHeight - headerHeight - footerHeight
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	body = fitToHeight(body, bodyHeight)

	content := strings.Join(append(append(headerLines, body), footerLines...), "\n")
	content = fitToHeight(content, innerHeight)
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
	m.screen = screenRun
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

func loadJobsCmd(ctx context.Context, client *jenkins.Client) tea.Cmd {
	return func() tea.Msg {
		if jobs, ok, err := cache.Jobs(client.CacheKey()); err == nil && ok {
			return jobsLoadedMsg{jobs: jobs, fromCache: true}
		}
		jobs, err := client.ListParameterizedJobs(ctx)
		if err != nil {
			return jobsLoadedMsg{jobs: jobs, err: err}
		}
		_ = cache.SaveJobs(client.CacheKey(), jobs)
		return jobsLoadedMsg{jobs: jobs, fromCache: false}
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

func clip(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
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

func fitToHeight(s string, height int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	if len(lines) < height {
		lines = append(lines, make([]string, height-len(lines))...)
	}
	return strings.Join(lines, "\n")
}

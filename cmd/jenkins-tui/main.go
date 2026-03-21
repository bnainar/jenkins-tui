package main

import (
	"encoding/json"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"jenkins-tui/internal/config"
	"jenkins-tui/internal/credentials"
	"jenkins-tui/internal/jenkins"
	"jenkins-tui/internal/models"
	"jenkins-tui/internal/tui"
)

var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "trigger":
			runTrigger(os.Args[2:])
			return
		case "list":
			runList(os.Args[2:])
			return
		case "search":
			runSearch(os.Args[2:])
			return
		case "params":
			runParams(os.Args[2:])
			return
		}
	}

	configPathFlag := flag.String("config", "", "absolute path to jenkins config file (default: $JENKINS_TUI_CONFIG or XDG config path)")
	cacheDirFlag := flag.String("cache-dir", "", "absolute path for jobs cache (default: $JENKINS_TUI_CACHE_DIR or XDG cache path)")
	timeout := flag.Duration("timeout", 60*time.Second, "HTTP client timeout for Jenkins API requests")
	showVersion := flag.Bool("v", false, "print version information and exit")
	showVersionLong := flag.Bool("version", false, "print version information and exit")
	flag.Parse()
	if *showVersion || *showVersionLong {
		fmt.Printf("jenkins-tui %s\ncommit: %s\nbuilt: %s\n", version, commit, buildDate)
		return
	}

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

type triggerParams []string

func (p *triggerParams) String() string {
	return strings.Join(*p, ",")
}

func (p *triggerParams) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("param cannot be empty")
	}
	if !strings.Contains(value, "=") {
		return fmt.Errorf("param must be KEY=VALUE")
	}
	*p = append(*p, value)
	return nil
}

type triggerResult struct {
	Target      string            `json:"target"`
	Job         string            `json:"job"`
	Params      map[string]string `json:"params"`
	QueueURL    string            `json:"queueUrl,omitempty"`
	BuildURL    string            `json:"buildUrl,omitempty"`
	BuildNumber int               `json:"buildNumber,omitempty"`
	State       string            `json:"state,omitempty"`
	Result      string            `json:"result,omitempty"`
}

type searchResult struct {
	Target string           `json:"target"`
	Query  string           `json:"query"`
	Limit  int              `json:"limit"`
	Jobs   []models.JobNode `json:"jobs"`
}

type listResult struct {
	Target       string           `json:"target"`
	ContainerURL string           `json:"containerUrl"`
	Prefix       string           `json:"prefix"`
	Jobs         []models.JobNode `json:"jobs"`
}

type paramsResult struct {
	Target string            `json:"target"`
	Job    string            `json:"job"`
	Params []models.ParamDef `json:"params"`
}

func runTrigger(args []string) {
	fs := flag.NewFlagSet("trigger", flag.ExitOnError)
	configPathFlag := fs.String("config", "", "absolute path to jenkins config file")
	timeout := fs.Duration("timeout", 60*time.Second, "HTTP client timeout for Jenkins API requests")
	targetID := fs.String("target", "", "configured Jenkins target id")
	jobURL := fs.String("job", "", "full Jenkins job URL")
	wait := fs.Bool("wait", false, "wait for build completion")
	jsonOut := fs.Bool("json", true, "print JSON output")
	var params triggerParams
	fs.Var(&params, "param", "build parameter in KEY=VALUE form (repeatable)")
	fs.Parse(args)

	if strings.TrimSpace(*targetID) == "" {
		fatalf("trigger: --target is required")
	}
	if strings.TrimSpace(*jobURL) == "" {
		fatalf("trigger: --job is required")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	target, client := mustBuildClient(ctx, *configPathFlag, *timeout, *targetID)
	paramMap, err := parseParams(params)
	if err != nil {
		fatalf("param error: %v", err)
	}

	queueURL, err := client.TriggerBuild(ctx, *jobURL, paramMap)
	if err != nil {
		fatalf("trigger error: %v", err)
	}

	result := triggerResult{
		Target:   target.ID,
		Job:      strings.TrimSpace(*jobURL),
		Params:   paramMap,
		QueueURL: queueURL,
		State:    string(models.RunQueued),
	}

	if *wait {
		buildURL, num, err := client.ResolveQueue(ctx, queueURL)
		if err != nil {
			fatalJSONOrText(*jsonOut, result, fmt.Errorf("queue resolve error: %w", err))
		}
		result.BuildURL = buildURL
		result.BuildNumber = num
		result.State = string(models.RunRunning)

		buildResult, err := client.PollBuild(ctx, buildURL)
		if err != nil {
			fatalJSONOrText(*jsonOut, result, fmt.Errorf("build poll error: %w", err))
		}
		result.Result = buildResult
		result.State = buildResult
	}

	if *jsonOut {
		printJSON(result)
		return
	}

	fmt.Printf("target=%s\njob=%s\nqueue=%s\n", result.Target, result.Job, result.QueueURL)
	if result.BuildURL != "" {
		fmt.Printf("build=%s\nnumber=%d\n", result.BuildURL, result.BuildNumber)
	}
	if result.Result != "" {
		fmt.Printf("result=%s\n", result.Result)
	}
}

func runSearch(args []string) {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	configPathFlag := fs.String("config", "", "absolute path to jenkins config file")
	timeout := fs.Duration("timeout", 60*time.Second, "HTTP client timeout for Jenkins API requests")
	targetID := fs.String("target", "", "configured Jenkins target id")
	query := fs.String("query", "", "job search query")
	limit := fs.Int("limit", 20, "maximum number of matching jobs to return")
	jsonOut := fs.Bool("json", true, "print JSON output")
	fs.Parse(args)

	if strings.TrimSpace(*targetID) == "" {
		fatalf("search: --target is required")
	}
	if strings.TrimSpace(*query) == "" {
		fatalf("search: --query is required")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	target, client := mustBuildClient(ctx, *configPathFlag, *timeout, *targetID)
	jobs, err := client.SearchJobs(ctx, *query, *limit)
	if err != nil {
		fatalf("search error: %v", err)
	}

	result := searchResult{
		Target: target.ID,
		Query:  strings.TrimSpace(*query),
		Limit:  *limit,
		Jobs:   jobs,
	}
	if *jsonOut {
		printJSON(result)
		return
	}
	for _, job := range jobs {
		fmt.Printf("%s\t%s\t%s\n", job.FullName, job.Name, job.URL)
	}
}

func runList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	configPathFlag := fs.String("config", "", "absolute path to jenkins config file")
	timeout := fs.Duration("timeout", 60*time.Second, "HTTP client timeout for Jenkins API requests")
	targetID := fs.String("target", "", "configured Jenkins target id")
	containerURL := fs.String("url", "", "folder or Jenkins root URL to list (default: target host root)")
	prefix := fs.String("prefix", "", "logical folder prefix for full names")
	jsonOut := fs.Bool("json", true, "print JSON output")
	fs.Parse(args)

	if strings.TrimSpace(*targetID) == "" {
		fatalf("list: --target is required")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	target, client := mustBuildClient(ctx, *configPathFlag, *timeout, *targetID)
	resolvedContainerURL := strings.TrimSpace(*containerURL)
	if resolvedContainerURL == "" {
		resolvedContainerURL = target.Host
	}
	nodes, err := client.ListJobNodes(ctx, resolvedContainerURL, strings.TrimSpace(*prefix))
	if err != nil {
		fatalf("list error: %v", err)
	}

	result := listResult{
		Target:       target.ID,
		ContainerURL: resolvedContainerURL,
		Prefix:       strings.TrimSpace(*prefix),
		Jobs:         nodes,
	}
	if *jsonOut {
		printJSON(result)
		return
	}
	for _, job := range nodes {
		fmt.Printf("%s\t%s\t%s\t%s\n", job.Kind, job.FullName, job.Name, job.URL)
	}
}

func runParams(args []string) {
	fs := flag.NewFlagSet("params", flag.ExitOnError)
	configPathFlag := fs.String("config", "", "absolute path to jenkins config file")
	timeout := fs.Duration("timeout", 60*time.Second, "HTTP client timeout for Jenkins API requests")
	targetID := fs.String("target", "", "configured Jenkins target id")
	jobURL := fs.String("job", "", "full Jenkins job URL")
	jsonOut := fs.Bool("json", true, "print JSON output")
	fs.Parse(args)

	if strings.TrimSpace(*targetID) == "" {
		fatalf("params: --target is required")
	}
	if strings.TrimSpace(*jobURL) == "" {
		fatalf("params: --job is required")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	target, client := mustBuildClient(ctx, *configPathFlag, *timeout, *targetID)
	params, err := client.GetJobParams(ctx, *jobURL)
	if err != nil {
		fatalf("params error: %v", err)
	}

	result := paramsResult{
		Target: target.ID,
		Job:    strings.TrimSpace(*jobURL),
		Params: params,
	}
	if *jsonOut {
		printJSON(result)
		return
	}
	for _, param := range params {
		fmt.Printf("%s\t%s\tdefault=%s\n", param.Name, param.Kind, param.Default)
	}
}

func findTarget(cfg models.Config, id string) (models.JenkinsTarget, error) {
	for _, target := range cfg.Jenkins {
		if target.ID == strings.TrimSpace(id) {
			return target, nil
		}
	}
	return models.JenkinsTarget{}, fmt.Errorf("target %q not found in config", id)
}

func mustBuildClient(ctx context.Context, configPathFlag string, timeout time.Duration, targetID string) (models.JenkinsTarget, *jenkins.Client) {
	configPath, err := config.ResolvePath(configPathFlag)
	if err != nil {
		fatalf("config error: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fatalf("config error: %v", err)
	}

	target, err := findTarget(cfg, targetID)
	if err != nil {
		fatalf("%v", err)
	}

	token, err := credentials.NewManager().Resolve(target)
	if err != nil {
		fatalf("credential error: %v", err)
	}

	client := jenkins.NewClient(target, token, timeout)
	if err := client.ValidateConnection(ctx); err != nil {
		fatalf("connection error: %v", err)
	}
	return target, client
}

func parseParams(values []string) (map[string]string, error) {
	params := make(map[string]string, len(values))
	for _, value := range values {
		key, raw, ok := strings.Cut(value, "=")
		if !ok {
			return nil, fmt.Errorf("invalid param %q", value)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("invalid param %q", value)
		}
		params[key] = raw
	}
	return params, nil
}

func printJSON(value any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(value)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func fatalJSONOrText(jsonOut bool, partial triggerResult, err error) {
	if jsonOut {
		payload := map[string]any{
			"error": err.Error(),
			"partial": partial,
		}
		printJSON(payload)
		os.Exit(1)
	}
	fatalf("%v", err)
}

package jenkins

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"jenkins-tui/internal/models"
)

type Client struct {
	target models.JenkinsTarget
	http   *http.Client
	crumb  *crumb
	mu     sync.RWMutex
}

type crumb struct {
	Field string `json:"crumbRequestField"`
	Value string `json:"crumb"`
}

func NewClient(target models.JenkinsTarget, timeout time.Duration) *Client {
	transport := &http.Transport{}
	if target.InsecureSkipTLSVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &Client{
		target: target,
		http: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
	}
}

func (c *Client) Host() string {
	return strings.TrimRight(c.target.Host, "/")
}

func (c *Client) CacheKey() string {
	return c.Host() + "|" + c.target.Username
}

type jobNodeResp struct {
	Jobs []struct {
		Name  string `json:"name"`
		URL   string `json:"url"`
		Class string `json:"_class"`
	} `json:"jobs"`
}

func (c *Client) ListJobNodes(ctx context.Context, baseURL, prefix string) ([]models.JobNode, error) {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = c.Host()
	}
	api := strings.TrimRight(baseURL, "/") + "/api/json?tree=jobs[name,url,_class]"
	var resp jobNodeResp
	if err := c.getJSON(ctx, api, &resp); err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	out := make([]models.JobNode, 0, len(resp.Jobs))
	for _, j := range resp.Jobs {
		if seen[j.URL] {
			continue
		}
		seen[j.URL] = true
		full := strings.Trim(path.Join(prefix, j.Name), "/")
		kind := models.JobNodeJob
		if isFolderClass(j.Class) {
			kind = models.JobNodeFolder
		}
		out = append(out, models.JobNode{Name: j.Name, FullName: full, URL: j.URL, Kind: kind})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind == models.JobNodeFolder
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func isFolderClass(class string) bool {
	return strings.Contains(class, "Folder") ||
		strings.Contains(class, "organization") ||
		strings.Contains(class, "WorkflowMultiBranch")
}

type jobParamsResp struct {
	Actions []struct {
		ParameterDefinitions []paramDefWire `json:"parameterDefinitions"`
	} `json:"actions"`
	Property []struct {
		ParameterDefinitions []paramDefWire `json:"parameterDefinitions"`
	} `json:"property"`
}

type paramDefWire struct {
	Name                  string   `json:"name"`
	Description           string   `json:"description"`
	Type                  string   `json:"type"`
	Choices               []string `json:"choices"`
	DefaultParameterValue struct {
		Value any `json:"value"`
	} `json:"defaultParameterValue"`
}

func (c *Client) GetJobParams(ctx context.Context, jobURL string) ([]models.ParamDef, error) {
	api := strings.TrimRight(jobURL, "/") + "/api/json?tree=actions[parameterDefinitions[name,description,type,choices,defaultParameterValue[value]]],property[parameterDefinitions[name,description,type,choices,defaultParameterValue[value]]]"
	var resp jobParamsResp
	if err := c.getJSON(ctx, api, &resp); err != nil {
		return nil, err
	}
	defs := make([]models.ParamDef, 0)
	appendDefs := func(definitions []paramDefWire) {
		for _, p := range definitions {
			kind := mapParamType(p.Type)
			if kind == "" {
				continue
			}
			defs = append(defs, models.ParamDef{
				Name:        p.Name,
				Kind:        kind,
				Description: p.Description,
				Choices:     p.Choices,
				Default:     fmt.Sprintf("%v", p.DefaultParameterValue.Value),
			})
		}
	}
	for _, action := range resp.Actions {
		appendDefs(action.ParameterDefinitions)
	}
	for _, prop := range resp.Property {
		appendDefs(prop.ParameterDefinitions)
	}
	if len(defs) == 0 {
		return defs, nil
	}
	// Deduplicate by parameter name; prefer first occurrence.
	seen := map[string]bool{}
	uniq := make([]models.ParamDef, 0, len(defs))
	for _, d := range defs {
		if seen[d.Name] {
			continue
		}
		seen[d.Name] = true
		uniq = append(uniq, d)
	}
	return uniq, nil
}

func mapParamType(t string) models.ParamKind {
	switch {
	case strings.Contains(t, "ChoiceParameterDefinition"):
		return models.ParamChoice
	case strings.Contains(t, "StringParameterDefinition"):
		return models.ParamString
	case strings.Contains(t, "TextParameterDefinition"):
		return models.ParamText
	case strings.Contains(t, "BooleanParameterDefinition"):
		return models.ParamBoolean
	case strings.Contains(t, "PasswordParameterDefinition"):
		return models.ParamPassword
	default:
		return ""
	}
}

func (c *Client) TriggerBuild(ctx context.Context, jobURL string, params map[string]string) (string, error) {
	if err := c.ensureCrumb(ctx); err != nil {
		return "", err
	}
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	triggerURL := strings.TrimRight(jobURL, "/") + "/buildWithParameters"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, triggerURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(c.target.Username, c.target.Token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if field, value, ok := c.crumbHeader(); ok {
		req.Header.Set(field, value)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("trigger failed (%d): %s", resp.StatusCode, string(body))
	}
	queueURL := resp.Header.Get("Location")
	if queueURL == "" {
		return "", fmt.Errorf("trigger succeeded but queue location missing")
	}
	return queueURL, nil
}

type queueResp struct {
	Executable *struct {
		Number int    `json:"number"`
		URL    string `json:"url"`
	} `json:"executable"`
	Cancelled bool `json:"cancelled"`
}

func (c *Client) ResolveQueue(ctx context.Context, queueURL string) (string, int, error) {
	api := strings.TrimRight(queueURL, "/") + "/api/json"
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	consecutiveErrors := 0
	for {
		select {
		case <-ctx.Done():
			return "", 0, ctx.Err()
		case <-ticker.C:
			var q queueResp
			if err := c.getJSON(ctx, api, &q); err != nil {
				consecutiveErrors++
				if consecutiveErrors >= 5 {
					return "", 0, fmt.Errorf("resolve queue failed after %d retries: %w", consecutiveErrors, err)
				}
				continue
			}
			consecutiveErrors = 0
			if q.Cancelled {
				return "", 0, fmt.Errorf("queue item cancelled")
			}
			if q.Executable != nil && q.Executable.URL != "" {
				return q.Executable.URL, q.Executable.Number, nil
			}
		}
	}
}

type buildResp struct {
	Building bool   `json:"building"`
	Result   string `json:"result"`
}

func (c *Client) PollBuild(ctx context.Context, buildURL string) (string, error) {
	api := strings.TrimRight(buildURL, "/") + "/api/json"
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	consecutiveErrors := 0
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			var b buildResp
			if err := c.getJSON(ctx, api, &b); err != nil {
				consecutiveErrors++
				if consecutiveErrors >= 5 {
					return "", fmt.Errorf("poll build failed after %d retries: %w", consecutiveErrors, err)
				}
				continue
			}
			consecutiveErrors = 0
			if !b.Building {
				if b.Result == "" {
					return "UNKNOWN", nil
				}
				return b.Result, nil
			}
		}
	}
}

func (c *Client) ensureCrumb(ctx context.Context) error {
	if _, _, ok := c.crumbHeader(); ok {
		return nil
	}
	crumbURL := c.Host() + "/crumbIssuer/api/json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, crumbURL, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.target.Username, c.target.Token)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("fetch crumb: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("fetch crumb failed (%d): %s", resp.StatusCode, string(body))
	}
	var cr crumb
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return fmt.Errorf("decode crumb response: %w", err)
	}
	if cr.Field != "" && cr.Value != "" {
		c.mu.Lock()
		if c.crumb == nil {
			c.crumb = &cr
		}
		c.mu.Unlock()
	}
	return nil
}

func (c *Client) crumbHeader() (string, string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.crumb == nil {
		return "", "", false
	}
	return c.crumb.Field, c.crumb.Value, true
}

func (c *Client) getJSON(ctx context.Context, endpoint string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, bytes.NewReader(nil))
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.target.Username, c.target.Token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GET %s failed (%d): %s", endpoint, resp.StatusCode, string(body))
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return err
	}
	return nil
}

package models

import "time"

type JenkinsTarget struct {
	Name                  string `yaml:"name"`
	Host                  string `yaml:"host"`
	Username              string `yaml:"username"`
	Token                 string `yaml:"token"`
	InsecureSkipTLSVerify bool   `yaml:"insecure_skip_tls_verify"`
}

type Config struct {
	Jenkins []JenkinsTarget `yaml:"jenkins"`
	Timeout time.Duration   `yaml:"-"`
}

type JobRef struct {
	Name     string
	FullName string
	URL      string
}

type ParamKind string

const (
	ParamChoice   ParamKind = "Choice"
	ParamString   ParamKind = "String"
	ParamText     ParamKind = "Text"
	ParamBoolean  ParamKind = "Boolean"
	ParamPassword ParamKind = "Password"
)

type ParamDef struct {
	Name        string
	Kind        ParamKind
	Description string
	Choices     []string
	Default     string
}

type JobSpec struct {
	Params map[string]string
}

type RunState string

const (
	RunPlanned RunState = "PLANNED"
	RunQueued  RunState = "QUEUED"
	RunRunning RunState = "RUNNING"
	RunSuccess RunState = "SUCCESS"
	RunFailed  RunState = "FAILED"
	RunAborted RunState = "ABORTED"
	RunError   RunState = "ERROR"
)

type RunRecord struct {
	Index       int
	Spec        JobSpec
	State       RunState
	QueueURL    string
	BuildURL    string
	BuildNumber int
	Result      string
	Err         string
	StartedAt   time.Time
	EndedAt     time.Time
}

type RunUpdate struct {
	Index       int
	State       RunState
	QueueURL    string
	BuildURL    string
	BuildNumber int
	Result      string
	Err         error
	Done        bool
}

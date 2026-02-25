package models

import "time"

type CredentialType string

const (
	CredentialTypeKeyring CredentialType = "keyring"
	CredentialTypeEnv     CredentialType = "env"
)

type Credential struct {
	Type CredentialType `yaml:"type"`
	Ref  string         `yaml:"ref"`
}

type JenkinsTarget struct {
	ID                    string     `yaml:"id"`
	Name                  string     `yaml:"name"`
	Host                  string     `yaml:"host"`
	Username              string     `yaml:"username"`
	Credential            Credential `yaml:"credential"`
	InsecureSkipTLSVerify bool       `yaml:"insecure_skip_tls_verify"`
}

type Config struct {
	Jenkins    []JenkinsTarget `yaml:"jenkins"`
	Timeout    time.Duration   `yaml:"-"`
	ConfigPath string          `yaml:"-"`
	CacheDir   string          `yaml:"-"`
}

type JobRef struct {
	Name     string
	FullName string
	URL      string
}

type JobNodeKind string

const (
	JobNodeFolder JobNodeKind = "folder"
	JobNodeJob    JobNodeKind = "job"
)

type JobNode struct {
	Name     string
	FullName string
	URL      string
	Kind     JobNodeKind
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

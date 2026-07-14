package result

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/lock"
)

const (
	SchemaVersion      = 1
	ExitSuccess        = 0
	ExitRuntimeFailure = 1
	ExitRejected       = 2
)

type ErrorKind string

const (
	ErrorRequest   ErrorKind = "request"
	ErrorPreflight ErrorKind = "preflight"
	ErrorRuntime   ErrorKind = "runtime"
)

type ResourceID struct {
	Kind      string `json:"kind"`
	Thread    string `json:"thread,omitempty"`
	Workdir   string `json:"workdir,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	Path      string `json:"path,omitempty"`
}

func WorkerResource(value string) (ResourceID, error) {
	thread, err := config.CanonicalThreadID(value)
	if err != nil {
		return ResourceID{}, err
	}
	return ResourceID{Kind: "worker", Thread: thread}, nil
}

func RunnerResource(value string) (ResourceID, error) {
	workdir, err := config.CanonicalWorkdir(value)
	if err != nil {
		return ResourceID{}, err
	}
	return ResourceID{Kind: "runner", Workdir: workdir}, nil
}

func WorkspaceResource(name string) ResourceID {
	return ResourceID{Kind: "workspace", Workspace: name}
}

func ConfigResource(path string) ResourceID {
	return ResourceID{Kind: "config", Path: path}
}

func ExecutableResource(path string) ResourceID {
	return ResourceID{Kind: "executable", Path: path}
}

func CommandResource() ResourceID {
	return ResourceID{Kind: "command"}
}

type Failure struct {
	Kind    ErrorKind       `json:"kind"`
	Message string          `json:"message"`
	Lock    *lock.BusyError `json:"lock,omitempty"`
}

type ExecutableDetails struct {
	Roles        []string `json:"roles"`
	Target       string   `json:"target"`
	Version      string   `json:"version,omitempty"`
	VersionError string   `json:"version_error,omitempty"`
	Selected     bool     `json:"selected,omitempty"`
}

type Outcome struct {
	Resource   ResourceID         `json:"resource"`
	Action     string             `json:"action"`
	Message    string             `json:"message,omitempty"`
	Executable *ExecutableDetails `json:"executable,omitempty"`
	Error      *Failure           `json:"error,omitempty"`
}

type Envelope struct {
	SchemaVersion int       `json:"schema_version"`
	Command       string    `json:"command"`
	DryRun        bool      `json:"dry_run"`
	Planned       []Outcome `json:"planned"`
	Successful    []Outcome `json:"successful"`
	Skipped       []Outcome `json:"skipped"`
	Failed        []Outcome `json:"failed"`
}

func NewEnvelope(command string, dryRun bool) Envelope {
	return Envelope{
		SchemaVersion: SchemaVersion,
		Command:       command,
		DryRun:        dryRun,
		Planned:       make([]Outcome, 0),
		Successful:    make([]Outcome, 0),
		Skipped:       make([]Outcome, 0),
		Failed:        make([]Outcome, 0),
	}
}

func (e Envelope) Write(w io.Writer) error {
	if e.SchemaVersion == 0 {
		e.SchemaVersion = SchemaVersion
	}
	if e.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported result schema version %d", e.SchemaVersion)
	}
	if e.Planned == nil {
		e.Planned = make([]Outcome, 0)
	}
	if e.Successful == nil {
		e.Successful = make([]Outcome, 0)
	}
	if e.Skipped == nil {
		e.Skipped = make([]Outcome, 0)
	}
	if e.Failed == nil {
		e.Failed = make([]Outcome, 0)
	}
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(e)
}

func (e Envelope) ExitCode() int {
	if len(e.Failed) == 0 {
		return ExitSuccess
	}
	for _, outcome := range e.Failed {
		if outcome.Error != nil && outcome.Error.Kind == ErrorRuntime {
			return ExitRuntimeFailure
		}
	}
	return ExitRejected
}

type CommandError struct {
	Kind ErrorKind
	Err  error
}

func (e *CommandError) Error() string {
	return e.Err.Error()
}

func (e *CommandError) Unwrap() error {
	return e.Err
}

func Request(err error) error {
	return commandError(ErrorRequest, err)
}

func Preflight(err error) error {
	return commandError(ErrorPreflight, err)
}

func Runtime(err error) error {
	return commandError(ErrorRuntime, err)
}

func commandError(kind ErrorKind, err error) error {
	if err == nil {
		return nil
	}
	var existing *CommandError
	if errors.As(err, &existing) {
		return err
	}
	return &CommandError{Kind: kind, Err: err}
}

func ErrorKindOf(err error) ErrorKind {
	var commandErr *CommandError
	if errors.As(err, &commandErr) {
		return commandErr.Kind
	}
	return ErrorRuntime
}

func ExitCode(err error) int {
	if err == nil {
		return ExitSuccess
	}
	switch ErrorKindOf(err) {
	case ErrorRequest, ErrorPreflight:
		return ExitRejected
	default:
		return ExitRuntimeFailure
	}
}

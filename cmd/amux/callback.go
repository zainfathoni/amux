package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/result"
	"github.com/zainfathoni/amux/internal/tmux"
)

const callbackSchemaVersion = 1

var (
	callbackNow            = func() time.Time { return time.Now().UTC() }
	callbackPaneByID       = func(id string) (tmux.WindowPane, error) { return (tmux.Runner{}).RestartPaneByID(id) }
	callbackInspectProcess = tmux.InspectProcess
	callbackSend           = func(id, token string) error { return (tmux.Runner{}).Notify(id, token) }
)

type callbackLease struct {
	ConfigDir       string    `json:"config_dir"`
	GroupID         string    `json:"group_id"`
	Coordinator     string    `json:"coordinator_thread"`
	PaneID          string    `json:"pane_id"`
	Session         string    `json:"session"`
	Window          string    `json:"window"`
	WindowID        string    `json:"window_id"`
	Dead            bool      `json:"dead"`
	Command         string    `json:"current_command"`
	StartCommand    string    `json:"start_command"`
	Workdir         string    `json:"canonical_workdir"`
	PID             int       `json:"pid"`
	ProcessName     string    `json:"process_name"`
	ProcessCommand  string    `json:"process_command"`
	ProcessIdentity string    `json:"process_identity"`
	PaneCreated     int64     `json:"pane_created"`
	Generation      int       `json:"generation"`
	RegisteredAt    time.Time `json:"registered_at"`
}

type callbackSlot struct {
	ConfigDir  string         `json:"config_dir"`
	GroupID    string         `json:"group_id"`
	Generation int            `json:"generation"`
	Lease      *callbackLease `json:"lease,omitempty"`
}

type callbackStore struct {
	SchemaVersion int            `json:"schema_version"`
	Slots         []callbackSlot `json:"slots"`
}

func (a app) executeCallback(in invocation, dir config.Directory) (*result.Envelope, error) {
	env := result.NewEnvelope(strings.Join(in.Path, " "), in.Options.DryRun)
	if in.Selectors.Group == "" {
		return &env, result.Request(errors.New("callback command requires --group"))
	}
	switch in.Command.Name {
	case "register":
		if in.Selectors.Thread == "" || in.Selectors.Pane == "" {
			return &env, result.Request(errors.New("callback register requires --group, --thread, and --pane"))
		}
		return a.registerCallback(in, dir, &env)
	case "clear":
		if in.Selectors.Thread != "" || in.Selectors.Pane != "" {
			return &env, result.Request(errors.New("callback clear accepts only --group"))
		}
		return a.clearCallback(in, dir, &env)
	default:
		return &env, result.Request(fmt.Errorf("unsupported callback command %s", in.Command.Name))
	}
}

func (a app) registerCallback(in invocation, dir config.Directory, env *result.Envelope) (*result.Envelope, error) {
	if err := requireGroupMembership(dir, in.Selectors.Group, in.Selectors.Thread, true); err != nil {
		return rejectCallback(env, dir, in.Selectors.Group, "register", result.ErrorPreflight, err)
	}
	lease, err := inspectCallbackLease(dir, in.Selectors.Group, in.Selectors.Thread, in.Selectors.Pane)
	if err != nil {
		return rejectCallback(env, dir, in.Selectors.Group, "register", result.ErrorPreflight, err)
	}
	path, err := callbackRuntimePath()
	if err != nil {
		return rejectCallback(env, dir, in.Selectors.Group, "register", result.ErrorPreflight, err)
	}
	store, err := loadCallbackStore(path)
	if err != nil {
		return rejectCallback(env, dir, in.Selectors.Group, "register", result.ErrorPreflight, err)
	}
	index := callbackSlotIndex(store, dir.Path, in.Selectors.Group)
	generation := 1
	if index >= 0 {
		generation = store.Slots[index].Generation + 1
	}
	lease.Generation = generation
	lease.RegisteredAt = callbackNow()
	if lease.RegisteredAt.IsZero() {
		return rejectCallback(env, dir, lease.GroupID, "register", result.ErrorPreflight, errors.New("callback registration time is unavailable"))
	}
	out := callbackOutcome(*lease, "register")
	if in.Options.DryRun {
		out.Message = "coordinator callback lease would be registered"
		env.Planned = append(env.Planned, out)
	} else {
		slot := callbackSlot{ConfigDir: dir.Path, GroupID: lease.GroupID, Generation: generation, Lease: lease}
		if index < 0 {
			store.Slots = append(store.Slots, slot)
		} else {
			store.Slots[index] = slot
		}
		if err := writeCallbackStore(path, store); err != nil {
			return rejectCallback(env, dir, lease.GroupID, "register", result.ErrorRuntime, err)
		}
		out.Action = "registered"
		out.Message = "coordinator callback lease registered; notification is not acknowledgement"
		env.Successful = append(env.Successful, out)
	}
	if !in.Options.JSON {
		fmt.Fprintf(a.stdout, "%s\t%s\t%d\t%s\n", lease.GroupID, outcomeLabelCallback(in.Options.DryRun, "registered"), generation, lease.PaneID)
	}
	return env, nil
}

func (a app) clearCallback(in invocation, dir config.Directory, env *result.Envelope) (*result.Envelope, error) {
	path, err := callbackRuntimePath()
	if err != nil {
		return rejectCallback(env, dir, in.Selectors.Group, "clear", result.ErrorPreflight, err)
	}
	store, err := loadCallbackStore(path)
	if err != nil {
		return rejectCallback(env, dir, in.Selectors.Group, "clear", result.ErrorPreflight, err)
	}
	index := callbackSlotIndex(store, dir.Path, in.Selectors.Group)
	generation := 1
	if index >= 0 {
		generation = store.Slots[index].Generation + 1
	}
	resource := result.ResourceID{Kind: "callback", Group: in.Selectors.Group, Path: configIdentity(dir.Path)}
	out := result.Outcome{Resource: resource, Action: "clear", Callback: &result.CallbackDetails{ConfigDir: dir.Path, Generation: generation}}
	if in.Options.DryRun {
		out.Message = "callback lease would be invalidated"
		env.Planned = append(env.Planned, out)
	} else {
		slot := callbackSlot{ConfigDir: dir.Path, GroupID: in.Selectors.Group, Generation: generation}
		if index < 0 {
			store.Slots = append(store.Slots, slot)
		} else {
			store.Slots[index] = slot
		}
		if err := writeCallbackStore(path, store); err != nil {
			return rejectCallback(env, dir, in.Selectors.Group, "clear", result.ErrorRuntime, err)
		}
		out.Action = "cleared"
		out.Message = "callback lease invalidated"
		env.Successful = append(env.Successful, out)
	}
	if !in.Options.JSON {
		fmt.Fprintf(a.stdout, "%s\t%s\t%d\n", in.Selectors.Group, outcomeLabelCallback(in.Options.DryRun, "cleared"), generation)
	}
	return env, nil
}

func inspectCallbackLease(dir config.Directory, group, thread, paneID string) (*callbackLease, error) {
	if !strings.HasPrefix(paneID, "%") || len(paneID) == 1 {
		return nil, fmt.Errorf("pane ID %q must be an exact tmux pane ID such as %%16", paneID)
	}
	pane, err := callbackPaneByID(paneID)
	if err != nil {
		return nil, fmt.Errorf("inspect callback pane %s: %w", paneID, err)
	}
	workdir, err := config.CanonicalWorkdir(pane.Path)
	if err != nil {
		return nil, fmt.Errorf("canonicalize callback pane workdir: %w", err)
	}
	process, err := callbackInspectProcess(pane.PID)
	if err != nil {
		return nil, err
	}
	if pane.PaneID != paneID || pane.WindowID == "" || pane.Session == "" || pane.Window == "" || pane.Dead || pane.PID <= 0 {
		return nil, errors.New("callback pane is missing complete live tmux identity")
	}
	if pane.Command != "amp" || process.PID != pane.PID || process.Name != "amp" || !interactiveProcessCommandMatches(process.Command, thread) || process.Identity == "" {
		return nil, fmt.Errorf("callback pane is not the expected interactive amp process (tmux=%q process=%q command=%q)", pane.Command, process.Name, process.Command)
	}
	if !callbackStartCommandMatches(dir, pane, workdir, thread) {
		return nil, fmt.Errorf("callback pane start command does not match coordinator thread %s and workdir %s", thread, workdir)
	}
	return &callbackLease{ConfigDir: dir.Path, GroupID: group, Coordinator: thread, PaneID: pane.PaneID, Session: pane.Session, Window: pane.Window, WindowID: pane.WindowID, Dead: pane.Dead, Command: pane.Command, StartCommand: pane.StartCommand, Workdir: workdir, PID: pane.PID, ProcessName: process.Name, ProcessCommand: process.Command, ProcessIdentity: process.Identity, PaneCreated: pane.StartTime}, nil
}

func callbackStartCommandMatches(dir config.Directory, pane tmux.WindowPane, workdir, thread string) bool {
	start := normalizedTmuxStartCommand(pane.StartCommand)
	if start == tmux.ContinueCommand(workdir, thread) {
		return true
	}
	rows, err := config.LoadReadOnly(dir.WorkersPath())
	if err != nil {
		return false
	}
	for _, row := range rows {
		rowWorkdir, err := config.CanonicalWorkdir(row.Workdir)
		if err != nil || row.Thread != thread || row.Workspace != pane.Session || row.Window != pane.Window || rowWorkdir != workdir {
			continue
		}
		expected := tmux.ContinueCommandWithEnv(workdir, thread, map[string]string{"AMUX_WORKSPACE": row.Workspace, "AMUX_SESSION": pane.Session, "AMUX_WINDOW": pane.Window, "AMUX_THREAD_ID": thread, "AMUX_WORKDIR": workdir})
		if start == expected {
			return true
		}
	}
	return false
}

func notifyReportCallback(dir config.Directory, group, reportID string) (result.Outcome, error) {
	resource := result.ResourceID{Kind: "callback", Group: group, Path: reportID}
	path, err := callbackRuntimePath()
	if err != nil {
		return result.Outcome{Resource: resource, Action: "notify"}, err
	}
	store, err := loadCallbackStore(path)
	if err != nil {
		return result.Outcome{Resource: resource, Action: "notify"}, err
	}
	index := callbackSlotIndex(store, dir.Path, group)
	if index < 0 || store.Slots[index].Lease == nil {
		return result.Outcome{Resource: resource, Action: "notify"}, fmt.Errorf("no live callback lease is registered for group %s in config directory %s", group, dir.Path)
	}
	lease := *store.Slots[index].Lease
	out := callbackOutcome(lease, "notify")
	out.Resource.Path = reportID
	if lease.Generation != store.Slots[index].Generation || lease.ConfigDir != dir.Path || lease.GroupID != group {
		return out, errors.New("callback lease runtime identity is inconsistent")
	}
	if err := requireGroupMembership(dir, group, lease.Coordinator, true); err != nil {
		return out, fmt.Errorf("callback coordinator intent changed: %w", err)
	}
	fresh, err := inspectCallbackLease(dir, group, lease.Coordinator, lease.PaneID)
	if err != nil {
		return out, err
	}
	if !sameCallbackIdentity(lease, *fresh) {
		return out, errors.New("callback lease is stale: pane, window, thread, workdir, or process identity changed")
	}
	token := fmt.Sprintf("AMUX_REPORT group=%s report=%s", group, reportID)
	if err := callbackSend(lease.PaneID, token); err != nil {
		return out, fmt.Errorf("send callback notification: %w", err)
	}
	out.Action = "notified"
	out.Message = "wake-up token sent; report remains pending until explicit acknowledgement"
	out.Callback.Notified = true
	return out, nil
}

func sameCallbackIdentity(a, b callbackLease) bool {
	return a.ConfigDir == b.ConfigDir && a.GroupID == b.GroupID && a.Coordinator == b.Coordinator && a.PaneID == b.PaneID && a.Session == b.Session && a.Window == b.Window && a.WindowID == b.WindowID && !a.Dead && !b.Dead && a.Command == b.Command && a.StartCommand == b.StartCommand && a.Workdir == b.Workdir && a.PID == b.PID && a.ProcessName == b.ProcessName && a.ProcessCommand == b.ProcessCommand && a.ProcessIdentity == b.ProcessIdentity && a.PaneCreated == b.PaneCreated
}

func callbackRuntimePath() (string, error) {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		var err error
		base, err = os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("resolve callback runtime directory: %w", err)
		}
	}
	return filepath.Join(base, "amux", "callback-leases.json"), nil
}

func loadCallbackStore(path string) (callbackStore, error) {
	store := callbackStore{SchemaVersion: callbackSchemaVersion, Slots: []callbackSlot{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return store, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&store); err != nil {
		return store, fmt.Errorf("parse callback lease store: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return store, errors.New("callback lease store contains trailing data")
	}
	if store.SchemaVersion != callbackSchemaVersion {
		return store, fmt.Errorf("unsupported callback lease schema version %d", store.SchemaVersion)
	}
	seen := map[string]bool{}
	for _, slot := range store.Slots {
		key := slot.ConfigDir + "\x00" + slot.GroupID
		if slot.ConfigDir == "" || config.ValidateGroupID(slot.GroupID) != nil || slot.Generation <= 0 || seen[key] {
			return store, errors.New("callback lease store contains invalid or duplicate slots")
		}
		if slot.Lease != nil {
			if slot.Lease.ConfigDir != slot.ConfigDir || slot.Lease.GroupID != slot.GroupID || slot.Lease.Generation != slot.Generation {
				return store, errors.New("callback lease store contains inconsistent lease identity")
			}
			if err := validateStoredCallbackLease(*slot.Lease); err != nil {
				return store, err
			}
		}
		seen[key] = true
	}
	return store, nil
}

func validateStoredCallbackLease(lease callbackLease) error {
	workdir, workdirErr := config.CanonicalWorkdir(lease.Workdir)
	thread, threadErr := config.CanonicalThreadID(lease.Coordinator)
	if !filepath.IsAbs(lease.ConfigDir) || filepath.Clean(lease.ConfigDir) != lease.ConfigDir || lease.GroupID == "" || threadErr != nil || thread != lease.Coordinator || lease.PaneID == "" || lease.Session == "" || lease.Window == "" || lease.WindowID == "" || lease.Dead || lease.Command != "amp" || lease.StartCommand == "" || workdirErr != nil || workdir != lease.Workdir || lease.PID <= 0 || lease.ProcessName != "amp" || !interactiveProcessCommandMatches(lease.ProcessCommand, lease.Coordinator) || lease.ProcessIdentity == "" || lease.Generation <= 0 || lease.RegisteredAt.IsZero() {
		return errors.New("callback lease store contains incomplete lease metadata")
	}
	return nil
}

func interactiveProcessCommandMatches(command, thread string) bool {
	fields := strings.Fields(command)
	return len(fields) == 4 && filepath.Base(fields[0]) == "amp" && fields[1] == "threads" && fields[2] == "continue" && fields[3] == thread
}

func writeCallbackStore(path string, store callbackStore) error {
	sort.Slice(store.Slots, func(i, j int) bool {
		if store.Slots[i].ConfigDir != store.Slots[j].ConfigDir {
			return store.Slots[i].ConfigDir < store.Slots[j].ConfigDir
		}
		return store.Slots[i].GroupID < store.Slots[j].GroupID
	})
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".callback-leases-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func callbackSlotIndex(store callbackStore, configDir, group string) int {
	for i, slot := range store.Slots {
		if slot.ConfigDir == configDir && slot.GroupID == group {
			return i
		}
	}
	return -1
}

func callbackOutcome(lease callbackLease, action string) result.Outcome {
	return result.Outcome{Resource: result.ResourceID{Kind: "callback", Group: lease.GroupID, Thread: lease.Coordinator, Path: configIdentity(lease.ConfigDir)}, Action: action, Callback: &result.CallbackDetails{Generation: lease.Generation, ConfigDir: lease.ConfigDir, PaneID: lease.PaneID, Session: lease.Session, Window: lease.Window, WindowID: lease.WindowID, PID: lease.PID, RegisteredAt: formatReportTime(lease.RegisteredAt)}}
}

func rejectCallback(env *result.Envelope, dir config.Directory, group, action string, kind result.ErrorKind, err error) (*result.Envelope, error) {
	out := result.Outcome{Resource: result.ResourceID{Kind: "callback", Group: group, Path: configIdentity(dir.Path)}, Action: action, Error: &result.Failure{Kind: kind, Message: err.Error()}}
	env.Failed = append(env.Failed, out)
	if kind == result.ErrorRuntime {
		return env, result.Runtime(err)
	}
	return env, result.Preflight(err)
}

func configIdentity(path string) string {
	sum := sha256.Sum256([]byte(path))
	return fmt.Sprintf("%x", sum[:8])
}

func outcomeLabelCallback(dryRun bool, completed string) string {
	if dryRun {
		return "planned"
	}
	return completed
}

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/fullerzz/herdr-plugin-sesh/internal/config"
	"github.com/fullerzz/herdr-plugin-sesh/internal/model"
	"github.com/fullerzz/herdr-plugin-sesh/internal/state"
)

func TestVersionCommand(t *testing.T) {
	var out bytes.Buffer
	a := &App{Out: &out, Err: &bytes.Buffer{}}
	if err := a.Run(context.Background(), []string{"--version"}); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "herdr-sesh dev" {
		t.Fatalf("got %q", out.String())
	}
}

func TestConfigPathCommand(t *testing.T) {
	var out bytes.Buffer
	a := &App{Out: &out, Err: &bytes.Buffer{}}
	if err := a.Run(context.Background(), []string{"config", "path"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "sesh.toml") {
		t.Fatalf("got %q", out.String())
	}
}

func TestListIgnoresCorruptSessionCache(t *testing.T) {
	d := t.TempDir()
	cfgPath := filepath.Join(d, "sesh.toml")
	if err := os.WriteFile(cfgPath, []byte("cache = true\n[[session]]\nname = \"api\"\npath = \"/tmp/api\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(d, "state")
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "sessions.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_PLUGIN_STATE_DIR", stateDir)
	t.Setenv("HERDR_SESSION", "")

	var out, errb bytes.Buffer
	a := &App{Out: &out, Err: &errb}
	if err := a.Run(context.Background(), []string{"list", "--json", "--config", cfgPath}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"name": "api"`) {
		t.Fatalf("output = %q", out.String())
	}
	if !strings.Contains(errb.String(), "warning: ignoring session cache") {
		t.Fatalf("stderr = %q", errb.String())
	}
}

func TestListWarnsWhenSessionCacheCannotBeSaved(t *testing.T) {
	d := t.TempDir()
	cfgPath := filepath.Join(d, "sesh.toml")
	if err := os.WriteFile(cfgPath, []byte("cache = true\n[[session]]\nname = \"api\"\npath = \"/tmp/api\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(d, "state-file")
	if err := os.WriteFile(statePath, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_PLUGIN_STATE_DIR", statePath)
	t.Setenv("HERDR_SESSION", "")

	var out, errb bytes.Buffer
	a := &App{Out: &out, Err: &errb}
	if err := a.Run(context.Background(), []string{"list", "--json", "--config", cfgPath}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"name": "api"`) {
		t.Fatalf("output = %q", out.String())
	}
	if !strings.Contains(errb.String(), "warning: ignoring session cache") || !strings.Contains(errb.String(), "warning: could not save session cache") {
		t.Fatalf("stderr = %q", errb.String())
	}
}

func TestListCacheDoesNotMaskBlacklistedResults(t *testing.T) {
	d := t.TempDir()
	cfgPath := filepath.Join(d, "sesh.toml")
	if err := os.WriteFile(cfgPath, []byte(`cache = true
blacklist = ["^scratch$"]

[[session]]
name = "api"
path = "/tmp/api"

[[session]]
name = "scratch"
path = "/tmp/scratch"
`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_PLUGIN_STATE_DIR", filepath.Join(d, "state"))
	t.Setenv("HERDR_SESSION", "")

	if got := runListJSON(t, cfgPath, ""); len(got) != 1 || got[0].Name != "api" {
		t.Fatalf("normal sessions = %#v", got)
	}
	if got := runListJSON(t, cfgPath, "", "--blacklisted"); len(got) != 1 || got[0].Name != "scratch" {
		t.Fatalf("blacklisted sessions = %#v", got)
	}
}

func TestListCacheDoesNotMaskDuplicateResults(t *testing.T) {
	d := t.TempDir()
	cfgPath := filepath.Join(d, "sesh.toml")
	if err := os.WriteFile(cfgPath, []byte(`cache = true
sort_order = ["config", "zoxide"]

[[session]]
name = "api"
path = "/configured/api"
`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_PLUGIN_STATE_DIR", filepath.Join(d, "state"))
	t.Setenv("HERDR_SESSION", "")
	zoxideOutput := "42 /discovered/api\n"

	if got := runListJSON(t, cfgPath, zoxideOutput); len(got) != 1 {
		t.Fatalf("deduplicated sessions = %#v", got)
	}
	if got := runListJSON(t, cfgPath, zoxideOutput, "--hide-duplicates=false"); len(got) != 2 {
		t.Fatalf("duplicate sessions = %#v", got)
	}
}

func TestListCacheDoesNotCrossConfigFiles(t *testing.T) {
	d := t.TempDir()
	firstConfig := filepath.Join(d, "first.toml")
	secondConfig := filepath.Join(d, "second.toml")
	if err := os.WriteFile(firstConfig, []byte("cache = true\n[[session]]\nname = \"api\"\npath = \"/tmp/api\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondConfig, []byte("cache = true\n[[session]]\nname = \"web\"\npath = \"/tmp/web\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_PLUGIN_STATE_DIR", filepath.Join(d, "state"))
	t.Setenv("HERDR_SESSION", "")

	if got := runListJSON(t, firstConfig, ""); len(got) != 1 || got[0].Name != "api" {
		t.Fatalf("first config sessions = %#v", got)
	}
	if got := runListJSON(t, secondConfig, ""); len(got) != 1 || got[0].Name != "web" {
		t.Fatalf("second config sessions = %#v", got)
	}
}

func TestListCacheDistinguishesRelativeConfigsAcrossWorkingDirectories(t *testing.T) {
	d := t.TempDir()
	firstDir := filepath.Join(d, "first")
	secondDir := filepath.Join(d, "second")
	for dir, name := range map[string]string{firstDir: "api", secondDir: "web"} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		body := fmt.Sprintf("cache = true\n[[session]]\nname = %q\npath = %q\n", name, filepath.Join("/tmp", name))
		if err := os.WriteFile(filepath.Join(dir, "sesh.toml"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HERDR_PLUGIN_STATE_DIR", filepath.Join(d, "state"))
	t.Setenv("HERDR_SESSION", "")

	t.Chdir(firstDir)
	if got := runListJSON(t, "sesh.toml", ""); len(got) != 1 || got[0].Name != "api" {
		t.Fatalf("first config sessions = %#v", got)
	}
	t.Chdir(secondDir)
	if got := runListJSON(t, "sesh.toml", ""); len(got) != 1 || got[0].Name != "web" {
		t.Fatalf("second config sessions = %#v", got)
	}
}

func TestPickerJSONCommand(t *testing.T) {
	var out bytes.Buffer
	a := &App{Out: &out, Err: &bytes.Buffer{}}
	if err := a.Run(context.Background(), []string{"picker", "--json", "--config", filepath.Join("..", "..", "testdata", "sesh.toml")}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"name": "sesh"`) {
		t.Fatalf("output = %q", out.String())
	}
}

func TestPickerJSONAppliesDefaultStartupCommand(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "sesh.toml")
	if err := os.WriteFile(cfgPath, []byte(`[default_session]
startup_command = "printf default:{}"

[[session]]
name = "api"
path = "/tmp/api"
`), 0600); err != nil {
		t.Fatal(err)
	}

	sessions := runPickerJSON(t, cfgPath, "")
	if len(sessions) != 1 || sessions[0].StartupCommand != "printf default:{}" {
		t.Fatalf("sessions = %#v", sessions)
	}
}

func TestPickerJSONAppliesWildcardSettings(t *testing.T) {
	project := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(t.TempDir(), "sesh.toml")
	if err := os.WriteFile(cfgPath, []byte(`strict_mode = true

[[wildcard]]
pattern = "`+project+`"
startup_command = "printf wildcard:{}"
preview_command = "printf preview:{}"
disable_startup_command = true
windows = ["git"]

[[window]]
name = "git"
startup_script = "git status"
`), 0600); err != nil {
		t.Fatal(err)
	}

	sessions := runPickerJSON(t, cfgPath, "42 "+project+"\n")
	if len(sessions) != 1 {
		t.Fatalf("sessions = %#v", sessions)
	}
	s := sessions[0]
	if s.StartupCommand != "" || s.PreviewCommand != "printf preview:{}" || !s.DisableStartupCommand || !reflect.DeepEqual(s.WindowNames, []string{"git"}) {
		t.Fatalf("wildcard session = %#v", s)
	}
	if len(s.WindowConfigs) != 0 {
		t.Fatalf("window configs leaked into JSON: %#v", s.WindowConfigs)
	}
}

func TestPickerJSONExplicitFalseOverridesWildcardDisable(t *testing.T) {
	project := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(t.TempDir(), "sesh.toml")
	if err := os.WriteFile(cfgPath, []byte(`[default_session]
startup_command = "printf default:{}"

[[session]]
name = "project"
path = "`+project+`"
disable_startup_command = false

[[wildcard]]
pattern = "`+project+`"
startup_command = "printf wildcard:{}"
disable_startup_command = true
`), 0600); err != nil {
		t.Fatal(err)
	}

	sessions := runPickerJSON(t, cfgPath, "")
	if len(sessions) != 1 {
		t.Fatalf("sessions = %#v", sessions)
	}
	if sessions[0].DisableStartupCommand || sessions[0].StartupCommand != "printf wildcard:{}" {
		t.Fatalf("session = %#v", sessions[0])
	}
}

func TestCollectDirectPathUsesConfiguredDirLength(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "parent")
	target := filepath.Join(parent, "child")
	if err := os.MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	configureFakeSources(t, "")
	cfg := config.Default()
	cfg.DirLength = 2

	sessions, err := (&App{}).collectAllowUnavailableHerdr(context.Background(), cfg, target)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Name != filepath.Join("parent", "child") {
		t.Fatalf("sessions = %#v", sessions)
	}
}

func TestCollectPropagatesHerdrErrors(t *testing.T) {
	configureFakeSources(t, "")

	if _, err := (&App{}).collect(context.Background(), config.Default(), ""); err == nil {
		t.Fatal("collect succeeded when Herdr workspace listing failed")
	}
}

func TestPreviewCommandUsesExplicitConfig(t *testing.T) {
	d := t.TempDir()
	targetDir := filepath.Join(d, "target")
	if err := os.Mkdir(targetDir, 0700); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(d, "sesh.toml")
	if err := os.WriteFile(cfgPath, []byte("[default_session]\npreview_command = \"printf configured:%s {}\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fakeBin := filepath.Join(d, "bin")
	if err := os.MkdirAll(fakeBin, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"herdr", "zoxide"} {
		//nolint:gosec // test creates local executable fixtures.
		if err := os.WriteFile(filepath.Join(fakeBin, name), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	//nolint:gosec // test creates a local executable fixture.
	if err := os.WriteFile(filepath.Join(fakeBin, "eza"), []byte("#!/bin/sh\nprintf 'default:%s\\n' \"$*\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_BIN_PATH", filepath.Join(fakeBin, "herdr"))
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var out bytes.Buffer
	a := &App{Out: &out, Err: &bytes.Buffer{}}
	if err := a.Run(context.Background(), []string{"preview", "--config", cfgPath, targetDir}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "configured:") || !strings.Contains(out.String(), targetDir) {
		t.Fatalf("output = %q", out.String())
	}
}

func TestLastFocusesPreviousWorkspaceAndRotatesHistory(t *testing.T) {
	d := t.TempDir()
	stateDir := filepath.Join(d, "state")
	if err := state.SaveHistory(stateDir, state.History{Workspaces: []string{"current", "previous", "older"}}); err != nil {
		t.Fatal(err)
	}
	fakeHerdr := filepath.Join(d, "herdr")
	logPath := filepath.Join(d, "herdr.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" > \"$HERDR_FAKE_LOG\"\n"
	//nolint:gosec // test creates a local executable fixture.
	if err := os.WriteFile(fakeHerdr, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_BIN_PATH", fakeHerdr)
	t.Setenv("HERDR_FAKE_LOG", logPath)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", stateDir)
	t.Setenv("HERDR_SESSION", "")
	t.Setenv("HERDR_WORKSPACE_ID", "current")

	a := &App{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	if err := a.Run(context.Background(), []string{"last"}); err != nil {
		t.Fatal(err)
	}
	//nolint:gosec // logPath is a test-owned temp file.
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(log)); got != "workspace focus previous" {
		t.Fatalf("herdr args = %q", got)
	}
	m, err := state.LoadFocusMRU(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if m.WorkspaceCurrent != "previous" || m.WorkspaceLast != "current" {
		t.Fatalf("workspace pair current=%q last=%q", m.WorkspaceCurrent, m.WorkspaceLast)
	}
	h, err := state.LoadHistory(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Workspaces) < 2 || h.Workspaces[0] != "previous" || h.Workspaces[1] != "current" {
		t.Fatalf("workspaces=%#v", h.Workspaces)
	}
}

func TestLastAgentFocusesPreviousTab(t *testing.T) {
	d := t.TempDir()
	stateDir := filepath.Join(d, "state")
	if err := state.ObserveAgentFocus(stateDir, state.AgentRef{WorkspaceID: "ws", TabID: "tab-a"}); err != nil {
		t.Fatal(err)
	}
	if err := state.ObserveAgentFocus(stateDir, state.AgentRef{WorkspaceID: "ws", TabID: "tab-b"}); err != nil {
		t.Fatal(err)
	}
	fakeHerdr := filepath.Join(d, "herdr")
	logPath := filepath.Join(d, "herdr.log")
	script := "#!/bin/sh\nprintf '%s\n' \"$*\" > \"$HERDR_FAKE_LOG\"\n"
	//nolint:gosec // test creates a local executable fixture.
	if err := os.WriteFile(fakeHerdr, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_BIN_PATH", fakeHerdr)
	t.Setenv("HERDR_FAKE_LOG", logPath)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", stateDir)
	t.Setenv("HERDR_SESSION", "")
	t.Setenv("HERDR_WORKSPACE_ID", "ws")
	t.Setenv("HERDR_TAB_ID", "tab-b")

	a := &App{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	if err := a.Run(context.Background(), []string{"last-agent"}); err != nil {
		t.Fatal(err)
	}
	//nolint:gosec // logPath is a test-owned temp file.
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(log)); got != "tab focus tab-a" {
		t.Fatalf("herdr args = %q", got)
	}
	m, err := state.LoadFocusMRU(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if m.AgentCurrent.TabID != "tab-a" || m.AgentLast.TabID != "tab-b" {
		t.Fatalf("agent pair current=%#v last=%#v", m.AgentCurrent, m.AgentLast)
	}
}

func TestLastClearsUnavailableWorkspaceDestination(t *testing.T) {
	d := t.TempDir()
	stateDir := filepath.Join(d, "state")
	if err := state.ObserveWorkspaceFocus(stateDir, "current"); err != nil {
		t.Fatal(err)
	}
	if err := state.ObserveWorkspaceFocus(stateDir, "missing"); err != nil {
		t.Fatal(err)
	}
	if err := state.ObserveWorkspaceFocus(stateDir, "current"); err != nil {
		t.Fatal(err)
	}
	fakeHerdr := filepath.Join(d, "herdr")
	script := "#!/bin/sh\necho 'error: workspace not found: missing' >&2\nexit 1\n"
	//nolint:gosec // test creates a local executable fixture.
	if err := os.WriteFile(fakeHerdr, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_BIN_PATH", fakeHerdr)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", stateDir)
	t.Setenv("HERDR_SESSION", "")
	t.Setenv("HERDR_WORKSPACE_ID", "current")

	a := &App{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	if err := a.Run(context.Background(), []string{"last"}); err == nil {
		t.Fatal("expected focus failure")
	}
	m, err := state.LoadFocusMRU(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if m.WorkspaceLast != "" {
		t.Fatalf("workspace_last=%q want empty after unavailable clear", m.WorkspaceLast)
	}
}

func TestLastKeepsDestinationWhenFocusFailsTransiently(t *testing.T) {
	d := t.TempDir()
	stateDir := filepath.Join(d, "state")
	if err := state.ObserveWorkspaceFocus(stateDir, "previous"); err != nil {
		t.Fatal(err)
	}
	if err := state.ObserveWorkspaceFocus(stateDir, "current"); err != nil {
		t.Fatal(err)
	}
	fakeHerdr := filepath.Join(d, "herdr")
	script := "#!/bin/sh\necho 'herdr daemon is not running' >&2\nexit 1\n"
	//nolint:gosec // test creates a local executable fixture.
	if err := os.WriteFile(fakeHerdr, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_BIN_PATH", fakeHerdr)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", stateDir)
	t.Setenv("HERDR_SESSION", "")
	t.Setenv("HERDR_WORKSPACE_ID", "current")

	a := &App{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	if err := a.Run(context.Background(), []string{"last"}); err == nil {
		t.Fatal("expected focus failure")
	}
	m, err := state.LoadFocusMRU(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if m.WorkspaceLast != "previous" {
		t.Fatalf("workspace_last=%q want previous preserved across transient failure", m.WorkspaceLast)
	}
}

func TestHookPrefersLiveEventPayloadOverStaleAmbientEnv(t *testing.T) {
	d := t.TempDir()
	stateDir := filepath.Join(d, "state")
	t.Setenv("HERDR_PLUGIN_STATE_DIR", stateDir)
	t.Setenv("HERDR_SESSION", "")
	// Ambient env still points at the previous focus; stock Herdr 0.7.5 event JSON
	// carries the newly focused ids (captured from a live plugin hook).
	t.Setenv("HERDR_WORKSPACE_ID", "stale-ws")
	t.Setenv("HERDR_TAB_ID", "stale-tab")
	t.Setenv("HERDR_PANE_ID", "stale-pane")
	t.Setenv("HERDR_PLUGIN_EVENT_JSON", `{"event":"workspace_focused","data":{"type":"workspace_focused","workspace_id":"w2"}}`)

	a := &App{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	if err := a.Run(context.Background(), []string{"hook", "workspace.focused"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_PLUGIN_EVENT_JSON", `{"event":"tab_focused","data":{"type":"tab_focused","tab_id":"w2:t1","workspace_id":"w2"}}`)
	if err := a.Run(context.Background(), []string{"hook", "tab.focused"}); err != nil {
		t.Fatal(err)
	}

	m, err := state.LoadFocusMRU(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if m.WorkspaceCurrent != "w2" {
		t.Fatalf("workspace_current=%q want event payload w2, not ambient stale-ws", m.WorkspaceCurrent)
	}
	if m.AgentCurrent.TabID != "w2:t1" || m.AgentCurrent.WorkspaceID != "w2" {
		t.Fatalf("agent_current=%#v want event payload", m.AgentCurrent)
	}
}

func TestHookActionPathTogglesExactlyTwoTargets(t *testing.T) {
	d := t.TempDir()
	baseState := filepath.Join(d, "state")
	fakeHerdr := filepath.Join(d, "herdr")
	focusLog := filepath.Join(d, "focus.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$HERDR_FAKE_LOG\"\n"
	//nolint:gosec // test creates a local executable fixture.
	if err := os.WriteFile(fakeHerdr, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_BIN_PATH", fakeHerdr)
	t.Setenv("HERDR_FAKE_LOG", focusLog)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", baseState)
	t.Setenv("HERDR_SESSION", "") // default session keeps state at the plugin root

	a := &App{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	runHook := func(event, eventJSON, workspaceID, tabID, paneID string) {
		t.Helper()
		t.Setenv("HERDR_PLUGIN_EVENT", event)
		t.Setenv("HERDR_PLUGIN_EVENT_JSON", eventJSON)
		t.Setenv("HERDR_WORKSPACE_ID", workspaceID)
		t.Setenv("HERDR_TAB_ID", tabID)
		t.Setenv("HERDR_PANE_ID", paneID)
		t.Setenv("HERDR_PLUGIN_CONTEXT_JSON", fmt.Sprintf(
			`{"workspace_id":%q,"tab_id":%q,"focused_pane_id":%q,"invocation_source":"api","correlation_id":%q}`,
			workspaceID, tabID, paneID, event,
		))
		if err := a.Run(context.Background(), []string{"hook", event}); err != nil {
			t.Fatalf("hook %s: %v", event, err)
		}
	}

	// Visit three workspaces and three tabs the way stock Herdr delivers focus events.
	runHook("workspace.focused", `{"event":"workspace_focused","data":{"type":"workspace_focused","workspace_id":"w1"}}`, "w1", "w1:t1", "w1:p1")
	runHook("tab.focused", `{"event":"tab_focused","data":{"type":"tab_focused","tab_id":"w1:t1","workspace_id":"w1"}}`, "w1", "w1:t1", "w1:p1")
	runHook("workspace.focused", `{"event":"workspace_focused","data":{"type":"workspace_focused","workspace_id":"w2"}}`, "w2", "w2:t1", "w2:p1")
	runHook("tab.focused", `{"event":"tab_focused","data":{"type":"tab_focused","tab_id":"w2:t1","workspace_id":"w2"}}`, "w2", "w2:t1", "w2:p1")
	runHook("workspace.focused", `{"event":"workspace_focused","data":{"type":"workspace_focused","workspace_id":"w3"}}`, "w3", "w3:t1", "w3:p1")
	runHook("tab.focused", `{"event":"tab_focused","data":{"type":"tab_focused","tab_id":"w3:t1","workspace_id":"w3"}}`, "w3", "w3:t1", "w3:p1")
	runHook("tab.focused", `{"event":"tab_focused","data":{"type":"tab_focused","tab_id":"w3:t2","workspace_id":"w3"}}`, "w3", "w3:t2", "w3:p2")
	runHook("tab.focused", `{"event":"tab_focused","data":{"type":"tab_focused","tab_id":"w3:t3","workspace_id":"w3"}}`, "w3", "w3:t3", "w3:p3")

	// Clear event env so actions look like keybound plugin_action invocations.
	t.Setenv("HERDR_PLUGIN_EVENT", "")
	t.Setenv("HERDR_PLUGIN_EVENT_JSON", "")
	t.Setenv("HERDR_WORKSPACE_ID", "w3")
	t.Setenv("HERDR_TAB_ID", "w3:t3")
	t.Setenv("HERDR_PANE_ID", "w3:p3")
	t.Setenv("HERDR_PLUGIN_CONTEXT_JSON", `{"workspace_id":"w3","tab_id":"w3:t3","focused_pane_id":"w3:p3","invocation_source":"keybinding"}`)
	t.Setenv("HERDR_PLUGIN_ACTION_ID", "last")

	for i := 0; i < 4; i++ {
		if err := a.Run(context.Background(), []string{"last"}); err != nil {
			t.Fatalf("last #%d: %v", i+1, err)
		}
		m, err := state.LoadFocusMRU(baseState)
		if err != nil {
			t.Fatal(err)
		}
		wantCurrent, wantLast := "w2", "w3"
		if i%2 == 1 {
			wantCurrent, wantLast = "w3", "w2"
		}
		if m.WorkspaceCurrent != wantCurrent || m.WorkspaceLast != wantLast {
			t.Fatalf("last #%d pair current=%q last=%q want %q/%q (must not resurrect w1)", i+1, m.WorkspaceCurrent, m.WorkspaceLast, wantCurrent, wantLast)
		}
		t.Setenv("HERDR_WORKSPACE_ID", m.WorkspaceCurrent)
	}

	// Re-sync ambient env to the agent pair after workspace toggles.
	m, err := state.LoadFocusMRU(baseState)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_WORKSPACE_ID", m.AgentCurrent.WorkspaceID)
	t.Setenv("HERDR_TAB_ID", m.AgentCurrent.TabID)
	t.Setenv("HERDR_PANE_ID", m.AgentCurrent.PaneID)
	t.Setenv("HERDR_PLUGIN_ACTION_ID", "last-agent")

	for i := 0; i < 4; i++ {
		if err := a.Run(context.Background(), []string{"last-agent"}); err != nil {
			t.Fatalf("last-agent #%d: %v", i+1, err)
		}
		m, err := state.LoadFocusMRU(baseState)
		if err != nil {
			t.Fatal(err)
		}
		wantCurrent, wantLast := "w3:t2", "w3:t3"
		if i%2 == 1 {
			wantCurrent, wantLast = "w3:t3", "w3:t2"
		}
		if m.AgentCurrent.TabID != wantCurrent || m.AgentLast.TabID != wantLast {
			t.Fatalf("last-agent #%d pair current=%q last=%q want %q/%q (must not resurrect w3:t1)", i+1, m.AgentCurrent.TabID, m.AgentLast.TabID, wantCurrent, wantLast)
		}
		t.Setenv("HERDR_TAB_ID", m.AgentCurrent.TabID)
		t.Setenv("HERDR_PANE_ID", m.AgentCurrent.PaneID)
		t.Setenv("HERDR_WORKSPACE_ID", m.AgentCurrent.WorkspaceID)
	}

	//nolint:gosec // focusLog is a test-owned temp file.
	log, err := os.ReadFile(focusLog)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(log)), "\n")
	if len(lines) != 8 {
		t.Fatalf("focus calls=%d want 8 (4 workspace + 4 agent)\n%s", len(lines), log)
	}
}

func TestPluginStateDirScopesNonDefaultSessions(t *testing.T) {
	d := t.TempDir()
	baseState := filepath.Join(d, "state")
	t.Setenv("HERDR_PLUGIN_STATE_DIR", baseState)

	// Default session keeps the herdr-managed root (compat with existing installs).
	t.Setenv("HERDR_SESSION", "default")
	if got := pluginStateDir(); got != baseState {
		t.Fatalf("default state dir=%q want %q", got, baseState)
	}
	t.Setenv("HERDR_SESSION", "")
	if got := pluginStateDir(); got != baseState {
		t.Fatalf("empty session state dir=%q want %q", got, baseState)
	}

	// Lab/non-default sessions must not share history.json with default: both mint w1/w2 ids.
	t.Setenv("HERDR_SESSION", "fm-lab-herdr-sesh-toggl-1")
	want := filepath.Join(baseState, "sessions", "666d2d6c61622d68657264722d736573682d746f67676c2d31")
	if got := pluginStateDir(); got != want {
		t.Fatalf("lab state dir=%q want %q", got, want)
	}
	t.Setenv("HERDR_SESSION", "lab:a")
	colonDir := pluginStateDir()
	t.Setenv("HERDR_SESSION", "lab_a")
	underscoreDir := pluginStateDir()
	if colonDir == underscoreDir {
		t.Fatalf("distinct sessions share state dir %q", colonDir)
	}
	t.Setenv("HERDR_SESSION", "..")
	if got := pluginStateDir(); filepath.Dir(got) != filepath.Join(baseState, "sessions") {
		t.Fatalf("session state escaped sessions dir: %q", got)
	}
	t.Setenv("HERDR_SESSION", "fm-lab-herdr-sesh-toggl-1")

	a := &App{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	t.Setenv("HERDR_PLUGIN_EVENT_JSON", `{"event":"workspace_focused","data":{"type":"workspace_focused","workspace_id":"w2"}}`)
	t.Setenv("HERDR_WORKSPACE_ID", "w2")
	if err := a.Run(context.Background(), []string{"hook", "workspace.focused"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(baseState, "history.json")); !os.IsNotExist(err) {
		t.Fatalf("default root history should stay untouched, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(want, "history.json")); err != nil {
		t.Fatalf("lab session history missing: %v", err)
	}
}

func TestPickerSwitchDoesNotRestoreClosedCurrentWorkspace(t *testing.T) {
	d := t.TempDir()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", d)
	if err := state.SaveHistory(d, state.History{Workspaces: []string{"current", "older"}}); err != nil {
		t.Fatal(err)
	}
	if err := state.RemoveWorkspace(d, "current"); err != nil {
		t.Fatal(err)
	}

	a := &App{Err: &bytes.Buffer{}}
	a.recordWorkspaceSwitch(pickerSwitchSource("current", true), "target")

	h, err := state.LoadHistory(d)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"target", "older"}
	if !reflect.DeepEqual(h.Workspaces, want) {
		t.Fatalf("workspaces=%#v want %#v", h.Workspaces, want)
	}
}

func runPickerJSON(t *testing.T, cfgPath, zoxideOutput string) []model.Session {
	t.Helper()
	configureFakeSources(t, zoxideOutput)

	var out bytes.Buffer
	a := &App{Out: &out, Err: &bytes.Buffer{}}
	if err := a.Run(context.Background(), []string{"picker", "--json", "--config", cfgPath}); err != nil {
		t.Fatal(err)
	}
	var sessions []model.Session
	if err := json.Unmarshal(out.Bytes(), &sessions); err != nil {
		t.Fatalf("decode picker JSON: %v\n%s", err, out.String())
	}
	return sessions
}

func runListJSON(t *testing.T, cfgPath, zoxideOutput string, extraArgs ...string) []model.Session {
	t.Helper()
	configureFakeSources(t, zoxideOutput)

	args := append([]string{"list", "--json", "--config", cfgPath}, extraArgs...)
	var out bytes.Buffer
	a := &App{Out: &out, Err: &bytes.Buffer{}}
	if err := a.Run(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	var sessions []model.Session
	if err := json.Unmarshal(out.Bytes(), &sessions); err != nil {
		t.Fatalf("decode list JSON: %v\n%s", err, out.String())
	}
	return sessions
}

func configureFakeSources(t *testing.T, zoxideOutput string) {
	t.Helper()
	fakeBin := t.TempDir()
	for name, script := range map[string]string{
		"herdr":  "#!/bin/sh\nexit 1\n",
		"zoxide": "#!/bin/sh\nprintf '%s' \"$FAKE_ZOXIDE_OUTPUT\"\n",
	} {
		//nolint:gosec // test creates local executable fixtures.
		if err := os.WriteFile(filepath.Join(fakeBin, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HERDR_BIN_PATH", filepath.Join(fakeBin, "herdr"))
	t.Setenv("FAKE_ZOXIDE_OUTPUT", zoxideOutput)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

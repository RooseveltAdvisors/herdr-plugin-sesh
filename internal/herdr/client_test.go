package herdr

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

type recRunner struct{ calls [][]string }

func (r *recRunner) Run(_ context.Context, bin string, args ...string) ([]byte, []byte, error) {
	r.calls = append(r.calls, append([]string{bin}, args...))
	return []byte(`{"id":"ws1","label":"api","cwd":"/tmp/api"}`), nil, nil
}
func TestCLIClientConstructsWorkspaceCreate(t *testing.T) {
	rr := &recRunner{}
	c := &CLIClient{Bin: "/bin/herdr", Runner: rr}
	_, err := c.WorkspaceCreate(context.Background(), WorkspaceCreateRequest{CWD: "/tmp/api", Label: "api", Focus: true})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"/bin/herdr", "workspace", "create", "--cwd", "/tmp/api", "--label", "api"},
		{"/bin/herdr", "workspace", "focus", "ws1"},
	}
	if !reflect.DeepEqual(rr.calls, want) {
		t.Fatalf("got %#v want %#v", rr.calls, want)
	}
}

func TestCLIClientConstructsWorkspaceCreateNoFocus(t *testing.T) {
	rr := &recRunner{}
	c := &CLIClient{Bin: "/bin/herdr", Runner: rr}
	_, err := c.WorkspaceCreate(context.Background(), WorkspaceCreateRequest{CWD: "/tmp/api", Label: "api"})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"/bin/herdr", "workspace", "create", "--cwd", "/tmp/api", "--label", "api", "--no-focus"}}
	if !reflect.DeepEqual(rr.calls, want) {
		t.Fatalf("got %#v want %#v", rr.calls, want)
	}
}

func TestCLIClientDecodesWorkspaceListEnvelope(t *testing.T) {
	c := &CLIClient{Bin: "/bin/herdr", Runner: fixedRunner{stdout: []byte(`{"result":{"workspaces":[{"workspace_id":"w1","label":"api","agent_status":"working"}]}}`)}}
	got, err := c.WorkspaceList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "w1" || got[0].Label != "api" || got[0].AgentStatus != "working" {
		t.Fatalf("workspaces=%#v", got)
	}
}

func TestCLIClientDecodesWorkspaceListArray(t *testing.T) {
	c := &CLIClient{Bin: "/bin/herdr", Runner: fixedRunner{stdout: []byte(`[{"id":"w1","label":"api","agent_status":"blocked"}]`)}}
	got, err := c.WorkspaceList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "w1" || got[0].Label != "api" || got[0].AgentStatus != "blocked" {
		t.Fatalf("workspaces=%#v", got)
	}
}

func TestCLIClientDecodesWorkspaceCreateEnvelope(t *testing.T) {
	c := &CLIClient{Bin: "/bin/herdr", Runner: fixedRunner{stdout: []byte(`{"result":{"root_pane":{"cwd":"/tmp/api","pane_id":"p1"},"workspace":{"workspace_id":"w1","label":"api"}}}`)}}
	got, err := c.WorkspaceCreate(context.Background(), WorkspaceCreateRequest{CWD: "/tmp/api", Label: "api", Focus: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "w1" || got.Label != "api" || got.CWD != "/tmp/api" {
		t.Fatalf("workspace=%#v", got)
	}
}

func TestCLIClientDecodesTabCreateEnvelope(t *testing.T) {
	c := &CLIClient{Bin: "/bin/herdr", Runner: fixedRunner{stdout: []byte(`{"result":{"root_pane":{"cwd":"/tmp/api","pane_id":"p1"},"tab":{"tab_id":"w1:t2","workspace_id":"w1","label":"api"}}}`)}}
	got, err := c.TabCreate(context.Background(), TabCreateRequest{WorkspaceID: "w1", CWD: "/tmp/api", Label: "api", Focus: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "w1:t2" || got.WorkspaceID != "w1" || got.CWD != "/tmp/api" || got.PaneID != "p1" {
		t.Fatalf("tab=%#v", got)
	}
}

func TestCLIClientDecodesTabListArray(t *testing.T) {
	c := &CLIClient{Bin: "/bin/herdr", Runner: fixedRunner{stdout: []byte(`[{"id":"w1:t1","workspace_id":"w1","label":"api"}]`)}}
	got, err := c.TabList(context.Background(), "w1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "w1:t1" || got[0].WorkspaceID != "w1" || got[0].Label != "api" {
		t.Fatalf("tabs=%#v", got)
	}
}

func TestCLIClientDecodesPaneListEnvelope(t *testing.T) {
	c := &CLIClient{Bin: "/bin/herdr", Runner: fixedRunner{stdout: []byte(`{"result":{"panes":[{"pane_id":"p1","workspace_id":"w1","tab_id":"w1:t1","cwd":"/tmp/api","foreground_cwd":"/tmp/api/sub","focused":true}]}}`)}}
	got, err := c.PaneList(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "p1" || got[0].WorkspaceID != "w1" || got[0].ForegroundCWD != "/tmp/api/sub" || !got[0].Focused {
		t.Fatalf("panes=%#v", got)
	}
}

func TestCLIClientDecodesPaneCurrentEnvelope(t *testing.T) {
	c := &CLIClient{Bin: "/bin/herdr", Runner: fixedRunner{stdout: []byte(`{"result":{"pane":{"pane_id":"p1","workspace_id":"w1","tab_id":"w1:t1","cwd":"/tmp/api"}}}`)}}
	got, err := c.PaneCurrent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "p1" || got.WorkspaceID != "w1" || got.TabID != "w1:t1" || got.CWD != "/tmp/api" {
		t.Fatalf("pane=%#v", got)
	}
}
func TestFakeClientRecordsPaneRun(t *testing.T) {
	f := &FakeClient{}
	_ = f.PaneRun(context.Background(), "p1", "npm test")
	if f.PaneRuns[0] != "p1:npm test" {
		t.Fatal(f.PaneRuns)
	}
}

type fixedRunner struct {
	stdout []byte
	stderr []byte
	err    error
}

func (r fixedRunner) Run(context.Context, string, ...string) ([]byte, []byte, error) {
	return r.stdout, r.stderr, r.err
}

func TestCLIClientReturnsDecodeErrors(t *testing.T) {
	c := &CLIClient{Bin: "/bin/herdr", Runner: fixedRunner{stdout: []byte("not json")}}
	_, err := c.WorkspaceList(context.Background())
	if err == nil {
		t.Fatal("expected decode error")
	}
	if !strings.Contains(err.Error(), "decode herdr workspace list JSON") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCLIClientIncludesStderrOnCommandFailure(t *testing.T) {
	c := &CLIClient{Bin: "/bin/herdr", Runner: fixedRunner{stderr: []byte("boom\n"), err: errors.New("exit status 1")}}
	_, err := c.WorkspaceList(context.Background())
	if err == nil {
		t.Fatal("expected command error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func exitStatusError(t *testing.T) error {
	t.Helper()
	err := exec.Command("sh", "-c", "exit 1").Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("fixture error = %v, want *exec.ExitError", err)
	}
	return err
}

func TestIsMissingTargetClassifiesFocusFailures(t *testing.T) {
	exitErr := exitStatusError(t)
	cases := []struct {
		name   string
		stderr string
		err    error
		want   bool
	}{
		{name: "unknown workspace", stderr: "error: workspace not found: w7", err: exitErr, want: true},
		{name: "unknown tab", stderr: "Error: Tab not found", err: exitErr, want: true},
		{name: "structured code", stderr: `{"error":{"code":"pane_not_found"}}`, err: exitErr, want: true},
		{name: "daemon down", stderr: "error: could not connect to the herdr daemon", err: exitErr, want: false},
		{name: "wrapper without binary", stderr: "sh: 1: herdr: command not found", err: exitErr, want: false},
		{name: "usage error", stderr: "error: unknown flag --nope", err: exitErr, want: false},
		{name: "binary missing from path", stderr: "", err: &exec.Error{Name: "herdr", Err: exec.ErrNotFound}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &CLIClient{Bin: "/bin/herdr", Runner: fixedRunner{stderr: []byte(tc.stderr), err: tc.err}}
			err := c.WorkspaceFocus(context.Background(), "w7")
			if err == nil {
				t.Fatal("expected focus error")
			}
			if got := IsMissingTarget(err); got != tc.want {
				t.Fatalf("IsMissingTarget=%v want %v for %v", got, tc.want, err)
			}
		})
	}
}

func TestIsMissingTargetIgnoresCommandArguments(t *testing.T) {
	c := &CLIClient{Bin: "/bin/herdr", Runner: fixedRunner{stderr: []byte("error: daemon unavailable"), err: exitStatusError(t)}}
	err := c.WorkspaceFocus(context.Background(), "tab_not_found")
	if err == nil {
		t.Fatal("expected focus error")
	}
	if IsMissingTarget(err) {
		t.Fatalf("argv must not be classified as a missing target: %v", err)
	}
}

func TestIsMissingTargetRejectsUnrelatedErrors(t *testing.T) {
	if IsMissingTarget(nil) {
		t.Fatal("nil error is not a missing target")
	}
	if IsMissingTarget(errors.New("workspace not found")) {
		t.Fatal("a bare error is not a herdr command failure")
	}
}

package herdr

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recRunner struct{ calls [][]string }

func (r *recRunner) Run(_ context.Context, bin string, args ...string) ([]byte, []byte, error) {
	r.calls = append(r.calls, append([]string{bin}, args...))
	return []byte(`{"id":"ws1","label":"api","cwd":"/tmp/api"}`), nil, nil
}

func fakeHerdr(t *testing.T) (string, string) {
	t.Helper()
	fakeBin := t.TempDir()
	fakeHerdr := filepath.Join(fakeBin, "herdr")
	//nolint:gosec // test creates a local executable fixture.
	if err := os.WriteFile(fakeHerdr, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return fakeBin, fakeHerdr
}

func TestNewCLIClientFallsBackFromStaleBinPath(t *testing.T) {
	fakeBin, fakeHerdr := fakeHerdr(t)
	t.Setenv("HERDR_BIN_PATH", filepath.Join(t.TempDir(), "deleted-herdr"))
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if got := NewCLIClient().Bin; got != fakeHerdr {
		t.Fatalf("bin = %q, want %q", got, fakeHerdr)
	}
}

func TestNewCLIClientFallsBackFromEmptyBinPath(t *testing.T) {
	fakeBin, fakeHerdr := fakeHerdr(t)
	t.Setenv("HERDR_BIN_PATH", "")
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if got := NewCLIClient().Bin; got != fakeHerdr {
		t.Fatalf("bin = %q, want %q", got, fakeHerdr)
	}
}

func TestNewCLIClientHonorsValidBinPath(t *testing.T) {
	_, fakeHerdr := fakeHerdr(t)
	t.Setenv("HERDR_BIN_PATH", fakeHerdr)

	if got := NewCLIClient().Bin; got != fakeHerdr {
		t.Fatalf("bin = %q, want %q", got, fakeHerdr)
	}
}

func TestCLIClientConstructsWorkspaceCreate(t *testing.T) {
	rr := &recRunner{}
	c := &CLIClient{Bin: "/bin/herdr", Runner: rr}
	_, err := c.WorkspaceCreate(context.Background(), WorkspaceCreateRequest{CWD: "/tmp/api", Label: "api", Focus: true})
	require.NoError(t, err)
	want := [][]string{
		{"/bin/herdr", "workspace", "create", "--cwd", "/tmp/api", "--label", "api"},
		{"/bin/herdr", "workspace", "focus", "ws1"},
	}
	assert.Equal(t, want, rr.calls)
}

func TestCLIClientConstructsWorkspaceCreateNoFocus(t *testing.T) {
	rr := &recRunner{}
	c := &CLIClient{Bin: "/bin/herdr", Runner: rr}
	_, err := c.WorkspaceCreate(context.Background(), WorkspaceCreateRequest{CWD: "/tmp/api", Label: "api"})
	require.NoError(t, err)
	want := [][]string{{"/bin/herdr", "workspace", "create", "--cwd", "/tmp/api", "--label", "api", "--no-focus"}}
	assert.Equal(t, want, rr.calls)
}

func TestCLIClientConstructsWorkspaceClose(t *testing.T) {
	rr := &recRunner{}
	c := &CLIClient{Bin: "/bin/herdr", Runner: rr}
	require.NoError(t, c.WorkspaceClose(context.Background(), "ws1"))
	want := [][]string{{"/bin/herdr", "workspace", "close", "ws1"}}
	assert.Equal(t, want, rr.calls)
}

func TestCLIClientDecodesWorkspaceListEnvelope(t *testing.T) {
	c := &CLIClient{Bin: "/bin/herdr", Runner: fixedRunner{stdout: []byte(`{"result":{"workspaces":[{"workspace_id":"w1","label":"api","agent_status":"working"}]}}`)}}
	got, err := c.WorkspaceList(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "w1", got[0].ID)
	require.Equal(t, "api", got[0].Label)
	assert.Equal(t, "working", got[0].AgentStatus)
}

func TestCLIClientDecodesWorkspaceListWorktreeMetadata(t *testing.T) {
	c := &CLIClient{Bin: "/bin/herdr", Runner: fixedRunner{stdout: []byte(`{"result":{"workspaces":[{"workspace_id":"w-root","label":"project","worktree":{"checkout_path":"/repos/project","is_linked_worktree":false,"repo_key":"/repos/project/.git","repo_name":"project","repo_root":"/repos/project"}},{"workspace_id":"w-child","label":"feature","worktree":{"checkout_path":"/worktrees/feature","is_linked_worktree":true,"repo_key":"/repos/project/.git","repo_name":"project","repo_root":"/repos/project"}}]}}`)}}
	got, err := c.WorkspaceList(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2)
	root, child := got[0].Worktree, got[1].Worktree
	require.NotNil(t, root)
	require.False(t, root.IsLinkedWorktree)
	require.Equal(t, "/repos/project", root.CheckoutPath)
	require.Equal(t, "/repos/project/.git", root.RepoKey)
	require.Equal(t, "project", root.RepoName)
	require.Equal(t, "/repos/project", root.RepoRoot)
	require.NotNil(t, child)
	require.True(t, child.IsLinkedWorktree)
	require.Equal(t, "/worktrees/feature", child.CheckoutPath)
	assert.Equal(t, root.RepoKey, child.RepoKey)
}

func TestCLIClientDecodesWorkspaceListArrayWithoutWorktreeMetadata(t *testing.T) {
	c := &CLIClient{Bin: "/bin/herdr", Runner: fixedRunner{stdout: []byte(`[{"id":"w1","label":"api","agent_status":"blocked"}]`)}}
	got, err := c.WorkspaceList(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "w1", got[0].ID)
	require.Equal(t, "api", got[0].Label)
	require.Equal(t, "blocked", got[0].AgentStatus)
	require.Nil(t, got[0].Worktree)
}

func TestCLIClientDecodesWorkspaceCreateEnvelope(t *testing.T) {
	c := &CLIClient{Bin: "/bin/herdr", Runner: fixedRunner{stdout: []byte(`{"result":{"root_pane":{"cwd":"/tmp/api","pane_id":"p1"},"workspace":{"workspace_id":"w1","label":"api"}}}`)}}
	got, err := c.WorkspaceCreate(context.Background(), WorkspaceCreateRequest{CWD: "/tmp/api", Label: "api", Focus: true})
	require.NoError(t, err)
	require.Equal(t, "w1", got.ID)
	require.Equal(t, "api", got.Label)
	assert.Equal(t, "/tmp/api", got.CWD)
}

func TestCLIClientDecodesTabCreateEnvelope(t *testing.T) {
	c := &CLIClient{Bin: "/bin/herdr", Runner: fixedRunner{stdout: []byte(`{"result":{"root_pane":{"cwd":"/tmp/api","pane_id":"p1"},"tab":{"tab_id":"w1:t2","workspace_id":"w1","label":"api"}}}`)}}
	got, err := c.TabCreate(context.Background(), TabCreateRequest{WorkspaceID: "w1", CWD: "/tmp/api", Label: "api", Focus: true})
	require.NoError(t, err)
	require.Equal(t, "w1:t2", got.ID)
	require.Equal(t, "w1", got.WorkspaceID)
	require.Equal(t, "/tmp/api", got.CWD)
	assert.Equal(t, "p1", got.PaneID)
}

func TestCLIClientDecodesTabListArray(t *testing.T) {
	c := &CLIClient{Bin: "/bin/herdr", Runner: fixedRunner{stdout: []byte(`[{"id":"w1:t1","workspace_id":"w1","label":"api"}]`)}}
	got, err := c.TabList(context.Background(), "w1")
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "w1:t1", got[0].ID)
	require.Equal(t, "w1", got[0].WorkspaceID)
	assert.Equal(t, "api", got[0].Label)
}

func TestCLIClientDecodesPaneListEnvelope(t *testing.T) {
	c := &CLIClient{Bin: "/bin/herdr", Runner: fixedRunner{stdout: []byte(`{"result":{"panes":[{"pane_id":"p1","workspace_id":"w1","tab_id":"w1:t1","cwd":"/tmp/api","foreground_cwd":"/tmp/api/sub","focused":true}]}}`)}}
	got, err := c.PaneList(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "p1", got[0].ID)
	require.Equal(t, "w1", got[0].WorkspaceID)
	require.Equal(t, "/tmp/api/sub", got[0].ForegroundCWD)
	assert.True(t, got[0].Focused)
}

func TestCLIClientDecodesPaneCurrentEnvelope(t *testing.T) {
	c := &CLIClient{Bin: "/bin/herdr", Runner: fixedRunner{stdout: []byte(`{"result":{"pane":{"pane_id":"p1","workspace_id":"w1","tab_id":"w1:t1","cwd":"/tmp/api"}}}`)}}
	got, err := c.PaneCurrent(context.Background())
	require.NoError(t, err)
	require.Equal(t, "p1", got.ID)
	require.Equal(t, "w1", got.WorkspaceID)
	require.Equal(t, "w1:t1", got.TabID)
	assert.Equal(t, "/tmp/api", got.CWD)
}

func TestCLIClientPaneFocusedOmitsCallerPane(t *testing.T) {
	d := t.TempDir()
	bin := filepath.Join(d, "herdr")
	script := `#!/bin/sh
if [ -n "$HERDR_PANE_ID" ]; then
  printf '{"workspace_id":"caller"}\n'
else
  printf '{"workspace_id":"focused"}\n'
fi
`
	//nolint:gosec // test creates a local executable fixture.
	require.NoError(t, os.WriteFile(bin, []byte(script), 0700))
	t.Setenv("HERDR_PANE_ID", "stale-pane")
	c := &CLIClient{Bin: bin, Runner: ExecRunner{}}

	caller, err := c.PaneCurrent(context.Background())
	require.NoError(t, err)
	focused, err := c.PaneFocused(context.Background())
	require.NoError(t, err)
	require.Equal(t, "caller", caller.WorkspaceID)
	assert.Equal(t, "focused", focused.WorkspaceID)
}
func TestFakeClientRecordsPaneRun(t *testing.T) {
	f := &FakeClient{}
	_ = f.PaneRun(context.Background(), "p1", "npm test")
	assert.Equal(t, "p1:npm test", f.PaneRuns[0])
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
	require.Error(t, err)
	require.ErrorContains(t, err, "decode herdr workspace list JSON")
}

func TestCLIClientIncludesStderrOnCommandFailure(t *testing.T) {
	c := &CLIClient{Bin: "/bin/herdr", Runner: fixedRunner{stderr: []byte("boom\n"), err: errors.New("exit status 1")}}
	_, err := c.WorkspaceList(context.Background())
	require.Error(t, err)
	require.ErrorContains(t, err, "boom")
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

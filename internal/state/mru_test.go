package state

import (
	"path/filepath"
	"testing"
)

func TestWorkspaceToggleOnlyTwoMostRecent(t *testing.T) {
	d := t.TempDir()
	// A -> B -> C, then toggle must stay on B/C.
	if err := ObserveWorkspaceFocus(d, "A"); err != nil {
		t.Fatal(err)
	}
	if err := ObserveWorkspaceFocus(d, "B"); err != nil {
		t.Fatal(err)
	}
	if err := ObserveWorkspaceFocus(d, "C"); err != nil {
		t.Fatal(err)
	}

	got := make([]string, 0, 4)
	current := "C"
	for i := 0; i < 4; i++ {
		target, ok, err := WorkspaceToggleTarget(d, current)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatalf("toggle %d: missing target from %s", i, current)
		}
		if err := ConsumeWorkspaceToggle(d, current, target); err != nil {
			t.Fatal(err)
		}
		got = append(got, target)
		current = target
	}
	want := []string{"B", "C", "B", "C"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sequence=%v want %v", got, want)
		}
	}
}

func TestWorkspaceToggleAfterManualSelection(t *testing.T) {
	d := t.TempDir()
	if err := ObserveWorkspaceFocus(d, "A"); err != nil {
		t.Fatal(err)
	}
	if err := ObserveWorkspaceFocus(d, "B"); err != nil {
		t.Fatal(err)
	}
	// Deliberate manual selection B -> C replaces the pair with B/C.
	if err := ObserveWorkspaceFocus(d, "C"); err != nil {
		t.Fatal(err)
	}

	target, ok, err := WorkspaceToggleTarget(d, "C")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || target != "B" {
		t.Fatalf("target=%q ok=%v want B", target, ok)
	}
	if err := ConsumeWorkspaceToggle(d, "C", "B"); err != nil {
		t.Fatal(err)
	}
	target, ok, err = WorkspaceToggleTarget(d, "B")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || target != "C" {
		t.Fatalf("target=%q ok=%v want C", target, ok)
	}
}

func TestAgentToggleOnlyTwoMostRecent(t *testing.T) {
	d := t.TempDir()
	a := AgentRef{WorkspaceID: "ws", TabID: "tA", PaneID: "pA"}
	b := AgentRef{WorkspaceID: "ws", TabID: "tB", PaneID: "pB"}
	c := AgentRef{WorkspaceID: "ws", TabID: "tC", PaneID: "pC"}
	for _, ref := range []AgentRef{a, b, c} {
		if err := ObserveAgentFocus(d, ref); err != nil {
			t.Fatal(err)
		}
	}

	current := c
	got := make([]string, 0, 4)
	for i := 0; i < 4; i++ {
		target, ok, err := AgentToggleTarget(d, current)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatalf("toggle %d missing", i)
		}
		if err := ConsumeAgentToggle(d, current, target); err != nil {
			t.Fatal(err)
		}
		got = append(got, target.TabID)
		current = target
	}
	want := []string{"tB", "tC", "tB", "tC"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sequence=%v want %v", got, want)
		}
	}
}

func TestSameWorkspaceAgentDoesNotOverwriteWorkspaceMRU(t *testing.T) {
	d := t.TempDir()
	if err := ObserveWorkspaceFocus(d, "agents"); err != nil {
		t.Fatal(err)
	}
	if err := ObserveWorkspaceFocus(d, "workspace"); err != nil {
		t.Fatal(err)
	}
	if err := ObserveWorkspaceFocus(d, "agents"); err != nil {
		t.Fatal(err)
	}
	first := AgentRef{WorkspaceID: "agents", TabID: "t1"}
	second := AgentRef{WorkspaceID: "agents", TabID: "t2"}
	if err := ObserveAgentFocus(d, first); err != nil {
		t.Fatal(err)
	}
	if err := ObserveAgentFocus(d, second); err != nil {
		t.Fatal(err)
	}

	wsTarget, ok, err := WorkspaceToggleTarget(d, "agents")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || wsTarget != "workspace" {
		t.Fatalf("workspace target=%q ok=%v", wsTarget, ok)
	}
	agentTarget, ok, err := AgentToggleTarget(d, second)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || agentTarget.TabID != "t1" {
		t.Fatalf("agent target=%#v ok=%v", agentTarget, ok)
	}
}

func TestCrossWorkspaceAgentFocusPreservesExplicitWorkspaceMRU(t *testing.T) {
	d := t.TempDir()
	if err := ObserveWorkspaceFocus(d, "first-agent"); err != nil {
		t.Fatal(err)
	}
	if err := ObserveWorkspaceFocus(d, "workspace"); err != nil {
		t.Fatal(err)
	}
	if err := ObserveWorkspaceFocus(d, "first-agent"); err != nil {
		t.Fatal(err)
	}
	first := AgentRef{WorkspaceID: "first-agent", TabID: "t1"}
	second := AgentRef{WorkspaceID: "second-agent", TabID: "t2"}
	if err := ObserveAgentFocus(d, first); err != nil {
		t.Fatal(err)
	}

	// last-agent style jump into another workspace must not rewrite workspace MRU.
	if err := PrepareAgentJump(d, true); err != nil {
		t.Fatal(err)
	}
	if err := ObserveWorkspaceFocus(d, "second-agent"); err != nil {
		t.Fatal(err)
	}
	if err := ObserveAgentFocus(d, second); err != nil {
		t.Fatal(err)
	}
	if err := ConsumeAgentToggle(d, first, second); err != nil {
		t.Fatal(err)
	}

	m, err := LoadFocusMRU(d)
	if err != nil {
		t.Fatal(err)
	}
	if m.WorkspaceLast != "workspace" {
		t.Fatalf("workspace_last=%q want workspace", m.WorkspaceLast)
	}
	if m.WorkspaceCurrent != "first-agent" {
		// current stays first-agent because skip suppressed the workspace observation
		t.Fatalf("workspace_current=%q want first-agent", m.WorkspaceCurrent)
	}
	if !m.AgentLast.Same(first) || !m.AgentCurrent.Same(second) {
		t.Fatalf("agent pair current=%#v last=%#v", m.AgentCurrent, m.AgentLast)
	}

	// Live focus is second-agent after the jump, but workspace toggle still targets
	// the explicit workspace MRU destination.
	wsTarget, ok, err := WorkspaceToggleTarget(d, "second-agent")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || wsTarget != "workspace" {
		t.Fatalf("workspace target=%q ok=%v (mru current=%q last=%q)", wsTarget, ok, m.WorkspaceCurrent, m.WorkspaceLast)
	}
	if err := ConsumeWorkspaceToggle(d, "second-agent", "workspace"); err != nil {
		t.Fatal(err)
	}
	// Next toggle returns to the preserved explicit workspace current (first-agent),
	// not the agent-jump workspace.
	wsTarget, ok, err = WorkspaceToggleTarget(d, "workspace")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || wsTarget != "first-agent" {
		t.Fatalf("workspace target=%q ok=%v want first-agent", wsTarget, ok)
	}
}

func TestUnavailableWorkspaceDestinationIsCleared(t *testing.T) {
	d := t.TempDir()
	if err := ObserveWorkspaceFocus(d, "A"); err != nil {
		t.Fatal(err)
	}
	if err := ObserveWorkspaceFocus(d, "B"); err != nil {
		t.Fatal(err)
	}
	if err := ClearWorkspaceLast(d, "A"); err != nil {
		t.Fatal(err)
	}
	_, ok, err := WorkspaceToggleTarget(d, "B")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected no target after clearing unavailable destination")
	}
}

func TestUnavailableAgentDestinationIsCleared(t *testing.T) {
	d := t.TempDir()
	a := AgentRef{WorkspaceID: "ws", TabID: "tA"}
	b := AgentRef{WorkspaceID: "ws", TabID: "tB"}
	if err := ObserveAgentFocus(d, a); err != nil {
		t.Fatal(err)
	}
	if err := ObserveAgentFocus(d, b); err != nil {
		t.Fatal(err)
	}
	if err := ClearAgentLast(d, a); err != nil {
		t.Fatal(err)
	}
	_, ok, err := AgentToggleTarget(d, b)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected no target after clearing unavailable agent")
	}
}

func TestClosedWorkspaceClearsToggleState(t *testing.T) {
	d := t.TempDir()
	if err := ObserveWorkspaceFocus(d, "A"); err != nil {
		t.Fatal(err)
	}
	if err := ObserveWorkspaceFocus(d, "B"); err != nil {
		t.Fatal(err)
	}
	if err := ClearWorkspace(d, "A"); err != nil {
		t.Fatal(err)
	}
	m, err := LoadFocusMRU(d)
	if err != nil {
		t.Fatal(err)
	}
	if m.WorkspaceLast != "" {
		t.Fatalf("workspace_last=%q", m.WorkspaceLast)
	}
	if filepath.Base(d) == "" {
		t.Fatal("temp dir missing")
	}
}

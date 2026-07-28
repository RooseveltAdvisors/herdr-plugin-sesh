package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
)

// AgentRef identifies a tab/agent target for last-agent toggles.
type AgentRef struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
	TabID       string `json:"tab_id,omitempty"`
	PaneID      string `json:"pane_id,omitempty"`
}

func (a AgentRef) Empty() bool {
	return a.WorkspaceID == "" && a.TabID == "" && a.PaneID == ""
}

func (a AgentRef) Same(other AgentRef) bool {
	if a.TabID != "" && other.TabID != "" {
		return a.TabID == other.TabID
	}
	if a.PaneID != "" && other.PaneID != "" {
		return a.PaneID == other.PaneID
	}
	return !a.Empty() && a == other
}

// FocusMRU is the authoritative two-slot toggle state for workspaces and agents/tabs.
// Workspaces still keeps a longer recency list for picker sorting only.
type FocusMRU struct {
	Workspaces []string `json:"workspaces,omitempty"`

	WorkspaceCurrent string `json:"workspace_current,omitempty"`
	WorkspaceLast    string `json:"workspace_last,omitempty"`

	AgentCurrent AgentRef `json:"agent_current,omitempty"`
	AgentLast    AgentRef `json:"agent_last,omitempty"`

	// SkipWorkspaceID suppresses a single workspace.focused observation for that
	// exact workspace so last-agent can cross workspaces without corrupting the
	// workspace pair. Any other observation clears it instead of being swallowed.
	SkipWorkspaceID string `json:"skip_workspace_id,omitempty"`

	// Migrated records that the two-slot fields have been seeded from the legacy
	// workspaces list, so an intentionally cleared slot is never re-seeded.
	Migrated bool `json:"migrated,omitempty"`
}

const mruFile = "history.json"

func mruPath(dir string) string { return filepath.Join(dir, mruFile) }

func LoadFocusMRU(dir string) (FocusMRU, error) {
	var m FocusMRU
	if dir == "" {
		return m, nil
	}
	b, err := os.ReadFile(mruPath(dir))
	if os.IsNotExist(err) {
		m.Migrated = true
		return m, nil
	}
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		if isJSONDecodeError(err) {
			return FocusMRU{Migrated: true}, nil
		}
		return m, err
	}
	m.migrateFromList()
	return m, nil
}

func SaveFocusMRU(dir string, m FocusMRU) error {
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	m.Workspaces = dedupeWorkspaces(nil, m.Workspaces)
	return writeJSONFile(mruPath(dir), m)
}

// migrateFromList seeds two-slot fields from the legacy workspaces list exactly
// once, when state written before the two-slot schema is first loaded.
func (m *FocusMRU) migrateFromList() {
	if m.Migrated {
		return
	}
	m.Migrated = true
	m.seedFromList()
}

func (m *FocusMRU) seedFromList() {
	if m.WorkspaceCurrent == "" && len(m.Workspaces) > 0 {
		m.WorkspaceCurrent = m.Workspaces[0]
	}
	if m.WorkspaceLast == "" && len(m.Workspaces) > 1 {
		for _, id := range m.Workspaces[1:] {
			if id != "" && id != m.WorkspaceCurrent {
				m.WorkspaceLast = id
				break
			}
		}
	}
}

func withFocusMRULock(dir string, fn func(*FocusMRU) error) error {
	if dir == "" {
		var m FocusMRU
		return fn(&m)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	lockPath := filepath.Join(dir, "history.lock")
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lockFile.Close()
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer func() { _ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN) }()

	m, err := LoadFocusMRU(dir)
	if err != nil {
		return err
	}
	if err := fn(&m); err != nil {
		return err
	}
	return SaveFocusMRU(dir, m)
}

// ObserveWorkspaceFocus updates the workspace two-slot pair from an authoritative focus event.
func ObserveWorkspaceFocus(dir, workspaceID string) error {
	if workspaceID == "" {
		return nil
	}
	return withFocusMRULock(dir, func(m *FocusMRU) error {
		if skip := m.SkipWorkspaceID; skip != "" {
			m.SkipWorkspaceID = ""
			if skip == workspaceID {
				return nil
			}
		}
		if m.WorkspaceCurrent == workspaceID {
			m.noteWorkspace(workspaceID)
			return nil
		}
		if m.WorkspaceCurrent != "" {
			m.WorkspaceLast = m.WorkspaceCurrent
		}
		m.WorkspaceCurrent = workspaceID
		m.noteWorkspace(workspaceID)
		return nil
	})
}

// ObserveAgentFocus updates the agent/tab two-slot pair from an authoritative focus event.
func ObserveAgentFocus(dir string, ref AgentRef) error {
	if ref.Empty() || (ref.TabID == "" && ref.PaneID == "") {
		return nil
	}
	return withFocusMRULock(dir, func(m *FocusMRU) error {
		if m.AgentCurrent.Same(ref) {
			return nil
		}
		if !m.AgentCurrent.Empty() {
			m.AgentLast = m.AgentCurrent
		}
		m.AgentCurrent = ref
		return nil
	})
}

// PrepareAgentJump marks a focus observation for workspaceID to be ignored when
// last-agent must change workspaces without updating the workspace pair. An empty
// workspaceID clears any pending suppression.
func PrepareAgentJump(dir, workspaceID string) error {
	return withFocusMRULock(dir, func(m *FocusMRU) error {
		m.SkipWorkspaceID = workspaceID
		return nil
	})
}

// ClearWorkspaceSkip drops a pending workspace-focus suppression flag.
func ClearWorkspaceSkip(dir string) error {
	return PrepareAgentJump(dir, "")
}

// ClearWorkspace removes a closed workspace from toggle state and picker recency.
func ClearWorkspace(dir, workspaceID string) error {
	if workspaceID == "" {
		return nil
	}
	return withFocusMRULock(dir, func(m *FocusMRU) error {
		if m.WorkspaceCurrent == workspaceID {
			m.WorkspaceCurrent = m.WorkspaceLast
			m.WorkspaceLast = ""
		}
		if m.WorkspaceLast == workspaceID {
			m.WorkspaceLast = ""
		}
		filtered := m.Workspaces[:0]
		for _, id := range m.Workspaces {
			if id != workspaceID {
				filtered = append(filtered, id)
			}
		}
		m.Workspaces = filtered
		if m.AgentCurrent.WorkspaceID == workspaceID {
			m.AgentCurrent = AgentRef{}
		}
		if m.AgentLast.WorkspaceID == workspaceID {
			m.AgentLast = AgentRef{}
		}
		return nil
	})
}

// ClearAgent removes a closed tab/agent from toggle state.
func ClearAgent(dir string, ref AgentRef) error {
	if ref.Empty() {
		return nil
	}
	return withFocusMRULock(dir, func(m *FocusMRU) error {
		if m.AgentCurrent.Same(ref) {
			m.AgentCurrent = m.AgentLast
			m.AgentLast = AgentRef{}
		}
		if m.AgentLast.Same(ref) {
			m.AgentLast = AgentRef{}
		}
		return nil
	})
}

// WorkspaceToggleTarget returns the other workspace in the two-slot pair.
// currentWorkspaceID is the live focused workspace from Herdr context; it is used
// only to choose which side of the stored pair to jump to, and never rewrites the
// pair. That keeps last-agent cross-workspace jumps orthogonal to workspace MRU.
func WorkspaceToggleTarget(dir, currentWorkspaceID string) (string, bool, error) {
	m, err := LoadFocusMRU(dir)
	if err != nil {
		return "", false, err
	}
	target := m.WorkspaceLast
	if target == "" {
		return "", false, nil
	}
	if currentWorkspaceID != "" && target == currentWorkspaceID {
		target = m.WorkspaceCurrent
	}
	if target == "" || target == currentWorkspaceID {
		return "", false, nil
	}
	return target, true, nil
}

// ConsumeWorkspaceToggle swaps the stored workspace pair after a successful jump.
// The live fromID is ignored on purpose: the pair is authoritative so agent jumps
// that temporarily leave the focused workspace do not invent a new previous target.
func ConsumeWorkspaceToggle(dir, _, toID string) error {
	if toID == "" {
		return nil
	}
	return withFocusMRULock(dir, func(m *FocusMRU) error {
		if m.WorkspaceCurrent == toID {
			m.noteWorkspace(toID)
			return nil
		}
		prev := m.WorkspaceCurrent
		m.WorkspaceCurrent = toID
		if prev != "" {
			m.WorkspaceLast = prev
		}
		m.Workspaces = dedupeWorkspaces([]string{toID, prev}, m.Workspaces)
		return nil
	})
}

// ClearWorkspaceLast drops an unavailable saved workspace destination.
func ClearWorkspaceLast(dir, id string) error {
	if id == "" {
		return nil
	}
	return withFocusMRULock(dir, func(m *FocusMRU) error {
		if m.WorkspaceLast == id {
			m.WorkspaceLast = ""
		}
		filtered := m.Workspaces[:0]
		for _, existing := range m.Workspaces {
			if existing != id {
				filtered = append(filtered, existing)
			}
		}
		m.Workspaces = filtered
		return nil
	})
}

// AgentToggleTarget returns the other agent/tab in the two-slot pair.
func AgentToggleTarget(dir string, current AgentRef) (AgentRef, bool, error) {
	var (
		target AgentRef
		ok     bool
	)
	err := withFocusMRULock(dir, func(m *FocusMRU) error {
		if !current.Empty() && !m.AgentCurrent.Same(current) {
			if !m.AgentCurrent.Empty() {
				m.AgentLast = m.AgentCurrent
			}
			m.AgentCurrent = current
		}
		target = m.AgentLast
		if target.Empty() || target.Same(m.AgentCurrent) {
			ok = false
			return nil
		}
		ok = true
		return nil
	})
	return target, ok, err
}

// ConsumeAgentToggle swaps the agent pair after a successful jump.
func ConsumeAgentToggle(dir string, from, to AgentRef) error {
	if to.Empty() {
		return nil
	}
	return withFocusMRULock(dir, func(m *FocusMRU) error {
		if !from.Empty() {
			m.AgentLast = from
		} else if !m.AgentCurrent.Empty() && !m.AgentCurrent.Same(to) {
			m.AgentLast = m.AgentCurrent
		}
		m.AgentCurrent = to
		return nil
	})
}

// ClearAgentLast drops an unavailable saved agent/tab destination.
func ClearAgentLast(dir string, ref AgentRef) error {
	if ref.Empty() {
		return nil
	}
	return withFocusMRULock(dir, func(m *FocusMRU) error {
		if m.AgentLast.Same(ref) {
			m.AgentLast = AgentRef{}
		}
		return nil
	})
}

func (m *FocusMRU) noteWorkspace(id string) {
	if id == "" {
		return
	}
	m.Workspaces = dedupeWorkspaces([]string{id}, m.Workspaces)
}

// Compatibility wrappers keep older call sites working against the two-slot model.

func LoadHistory(dir string) (History, error) {
	m, err := LoadFocusMRU(dir)
	if err != nil {
		return History{}, err
	}
	return History{Workspaces: append([]string(nil), m.Workspaces...)}, nil
}

// SaveHistory replaces the picker recency list wholesale and seeds the two-slot
// pair from it. It has no production caller: the picker path only reads through
// LoadHistory. Because the seed bypasses the one-shot Migrated gate, any new
// caller must not run it after a destination has been deliberately cleared.
func SaveHistory(dir string, h History) error {
	return withFocusMRULock(dir, func(m *FocusMRU) error {
		m.Workspaces = append([]string(nil), h.Workspaces...)
		m.seedFromList()
		return nil
	})
}

func Record(dir, workspaceID string) error {
	return ObserveWorkspaceFocus(dir, workspaceID)
}

func RecordSwitch(dir, fromWorkspaceID, toWorkspaceID string) error {
	return ConsumeWorkspaceToggle(dir, fromWorkspaceID, toWorkspaceID)
}

func Last(dir string) (string, bool, error) {
	m, err := LoadFocusMRU(dir)
	if err != nil {
		return "", false, err
	}
	if m.WorkspaceLast == "" {
		return "", false, nil
	}
	return m.WorkspaceLast, true, nil
}

func Previous(dir, currentWorkspaceID string) (string, bool, error) {
	return WorkspaceToggleTarget(dir, currentWorkspaceID)
}

package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fullerzz/herdr-plugin-sesh/internal/state"
)

func TestWatchWorkspaceEventsReconcilesDelayedProtocol20Replay(t *testing.T) {
	listener, socketPath := listenTestSocket(t)
	serverDone := make(chan error, 1)
	go func() {
		stream, err := acceptRequest(listener, "events.subscribe")
		if err != nil {
			serverDone <- err
			return
		}
		enc := json.NewEncoder(stream)
		if err := enc.Encode(map[string]any{"id": "herdr-sesh-history", "result": map[string]any{"type": "subscription_started"}}); err != nil {
			_ = stream.Close()
			serverDone <- err
			return
		}
		if err := enc.Encode(workspaceEventMessage("workspace_focused", "older")); err != nil {
			_ = stream.Close()
			serverDone <- err
			return
		}
		if err := serveCompatiblePing(listener, 20); err != nil {
			_ = stream.Close()
			serverDone <- err
			return
		}

		snapshot, err := acceptRequest(listener, "session.snapshot")
		if err != nil {
			_ = stream.Close()
			serverDone <- err
			return
		}
		if err := json.NewEncoder(snapshot).Encode(snapshotResponse("current", "current", "older", "previous")); err != nil {
			_ = snapshot.Close()
			_ = stream.Close()
			serverDone <- err
			return
		}
		_ = snapshot.Close()

		// Protocol 20 can deliver another retained event after the initial
		// snapshot with no replay-boundary marker.
		if err := enc.Encode(workspaceEventMessage("workspace_focused", "previous")); err != nil {
			_ = stream.Close()
			serverDone <- err
			return
		}
		if err := enc.Encode(workspaceEventMessage("workspace_closed", "current")); err != nil {
			_ = stream.Close()
			serverDone <- err
			return
		}
		_ = stream.Close()
		serverDone <- serveProtocol20Snapshots(listener, "current", "current", "older", "previous")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var history []string
	reconnect, err := watchWorkspaceEventsOnce(ctx, socketPath,
		func(id string) error {
			history = recordHistoryVisit(history, id)
			return nil
		},
		func(id string) error {
			history = removeHistoryWorkspace(history, id)
			return nil
		},
	)
	_ = listener.Close()
	require.True(t, reconnect)
	require.Error(t, err)
	require.NoError(t, <-serverDone)
	want := []string{"current", "previous", "older"}
	assert.Equal(t, want, history)
}

func TestWatchWorkspaceEventsPreservesInterruptedProtocol20Replay(t *testing.T) {
	listener, socketPath := listenTestSocket(t)
	serverDone := make(chan error, 1)
	go func() {
		stream, err := acceptRequest(listener, "events.subscribe")
		if err != nil {
			serverDone <- err
			return
		}
		enc := json.NewEncoder(stream)
		if err := enc.Encode(map[string]any{"id": "herdr-sesh-history", "result": map[string]any{"type": "subscription_started"}}); err != nil {
			_ = stream.Close()
			serverDone <- err
			return
		}
		if err := serveCompatiblePing(listener, 20); err != nil {
			_ = stream.Close()
			serverDone <- err
			return
		}
		for _, event := range []map[string]any{
			workspaceEventMessage("workspace_focused", "older"),
			workspaceEventMessage("workspace_focused", "previous"),
			workspaceEventMessage("workspace_closed", "current"),
		} {
			if err := enc.Encode(event); err != nil {
				_ = stream.Close()
				serverDone <- err
				return
			}
		}
		_ = stream.Close()
		serverDone <- serveProtocol20Snapshots(listener, "current", "current", "older", "previous")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var history []string
	reconnect, err := watchWorkspaceEventsOnce(ctx, socketPath,
		func(id string) error {
			history = recordHistoryVisit(history, id)
			return nil
		},
		func(id string) error {
			history = removeHistoryWorkspace(history, id)
			return nil
		},
	)
	_ = listener.Close()
	require.True(t, reconnect)
	require.Error(t, err)
	require.NoError(t, <-serverDone)
	want := []string{"current", "previous", "older"}
	assert.Equal(t, want, history)
}

func TestWatchWorkspaceEventsRejectsPreSubscriptionProtocol(t *testing.T) {
	listener, socketPath := listenTestSocket(t)
	serverDone := make(chan error, 1)
	go func() {
		stream, err := acceptRequest(listener, "events.subscribe")
		if err != nil {
			serverDone <- err
			return
		}
		defer func() { _ = stream.Close() }()
		if err := json.NewEncoder(stream).Encode(map[string]any{
			"id": "herdr-sesh-history", "result": map[string]any{"type": "subscription_started"},
		}); err != nil {
			serverDone <- err
			return
		}
		conn, err := acceptRequest(listener, "ping")
		if err != nil {
			serverDone <- err
			return
		}
		defer func() { _ = conn.Close() }()
		serverDone <- json.NewEncoder(conn).Encode(map[string]any{
			"id": "herdr-sesh-history-ping",
			"result": map[string]any{
				"type": "pong", "version": "0.8.1", "protocol": 19,
			},
		})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := WatchWorkspaceEvents(ctx, socketPath, func(string) error { return nil }, func(string) error { return nil })
	require.ErrorContains(t, err, "requires Herdr protocol 20")
	require.NoError(t, <-serverDone)
}

func TestWatchWorkspaceEventsBuffersProtocol21EventsDuringProtocolProbe(t *testing.T) {
	listener, socketPath := listenTestSocket(t)
	serverDone := make(chan error, 1)
	go func() {
		first, method, err := acceptAnyRequest(listener)
		if err != nil {
			serverDone <- err
			return
		}
		var stream net.Conn
		switch method {
		case "events.subscribe":
			stream = first
			if err := json.NewEncoder(stream).Encode(map[string]any{
				"id": "herdr-sesh-history", "result": map[string]any{"type": "subscription_started"},
			}); err != nil {
				serverDone <- err
				return
			}
			ping, err := acceptRequest(listener, "ping")
			if err != nil {
				serverDone <- err
				return
			}
			for _, workspaceID := range []string{"B", "C"} {
				if err := json.NewEncoder(stream).Encode(workspaceEventMessage("workspace_focused", workspaceID)); err != nil {
					_ = ping.Close()
					serverDone <- err
					return
				}
			}
			if err := encodeCompatiblePing(ping, 21); err != nil {
				_ = ping.Close()
				serverDone <- err
				return
			}
			_ = ping.Close()
		case "ping":
			if err := encodeCompatiblePing(first, 21); err != nil {
				_ = first.Close()
				serverDone <- err
				return
			}
			_ = first.Close()
			stream, err = acceptRequest(listener, "events.subscribe")
			if err != nil {
				serverDone <- err
				return
			}
			if err := json.NewEncoder(stream).Encode(map[string]any{
				"id": "herdr-sesh-history", "result": map[string]any{"type": "subscription_started"},
			}); err != nil {
				serverDone <- err
				return
			}
		default:
			_ = first.Close()
			serverDone <- fmt.Errorf("first method=%q want events.subscribe or ping", method)
			return
		}
		defer func() { _ = stream.Close() }()

		snapshot, err := acceptRequest(listener, "session.snapshot")
		if err != nil {
			serverDone <- err
			return
		}
		serverDone <- json.NewEncoder(snapshot).Encode(snapshotResponse("C", "B", "C"))
		_ = snapshot.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	history := []string{"A"}
	reconnect, err := watchWorkspaceEventsOnce(ctx, socketPath,
		func(id string) error {
			history = recordHistoryVisit(history, id)
			return nil
		},
		func(id string) error {
			history = removeHistoryWorkspace(history, id)
			return nil
		},
	)
	require.True(t, reconnect)
	require.Error(t, err)
	require.NoError(t, <-serverDone)
	assert.Equal(t, []string{"C", "B", "A"}, history)
}

func TestWatchWorkspaceEventsNoHistoryKeepsSnapshotWindowEvent(t *testing.T) {
	listener, socketPath := listenTestSocket(t)
	serverDone := make(chan error, 1)
	go func() {
		stream, err := acceptRequest(listener, "events.subscribe")
		if err != nil {
			serverDone <- err
			return
		}
		defer func() { _ = stream.Close() }()
		enc := json.NewEncoder(stream)
		if err := enc.Encode(map[string]any{"id": "herdr-sesh-history", "result": map[string]any{"type": "subscription_started"}}); err != nil {
			serverDone <- err
			return
		}
		if err := serveCompatiblePing(listener, 21); err != nil {
			serverDone <- err
			return
		}
		snapshot, err := acceptRequest(listener, "session.snapshot")
		if err != nil {
			serverDone <- err
			return
		}
		defer func() { _ = snapshot.Close() }()
		if err := enc.Encode(workspaceEventMessage("workspace_focused", "during-snapshot")); err != nil {
			serverDone <- err
			return
		}
		serverDone <- json.NewEncoder(snapshot).Encode(snapshotResponse("snapshot", "snapshot"))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var got []string
	err := WatchWorkspaceEvents(ctx, socketPath,
		func(id string) error {
			got = append(got, id)
			if len(got) == 2 {
				cancel()
			}
			return nil
		},
		func(string) error { return nil },
	)
	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, <-serverDone)
	assert.Equal(t, []string{"snapshot", "during-snapshot"}, got)
}

func TestWatchWorkspaceEventsReconnectsAfterUnexpectedEOF(t *testing.T) {
	listener, socketPath := listenTestSocket(t)
	serverDone := make(chan error, 1)
	go func() {
		for attempt := 0; attempt < 2; attempt++ {
			stream, err := acceptRequest(listener, "events.subscribe")
			if err != nil {
				serverDone <- err
				return
			}
			enc := json.NewEncoder(stream)
			if err := enc.Encode(map[string]any{"id": "herdr-sesh-history", "result": map[string]any{"type": "subscription_started"}}); err != nil {
				_ = stream.Close()
				serverDone <- err
				return
			}
			if err := serveCompatiblePing(listener, 21); err != nil {
				_ = stream.Close()
				serverDone <- err
				return
			}
			snapshot, err := acceptRequest(listener, "session.snapshot")
			if err != nil {
				_ = stream.Close()
				serverDone <- err
				return
			}
			if err := json.NewEncoder(snapshot).Encode(snapshotResponse("")); err != nil {
				_ = snapshot.Close()
				_ = stream.Close()
				serverDone <- err
				return
			}
			_ = snapshot.Close()
			if attempt == 1 {
				if err := enc.Encode(workspaceEventMessage("workspace_focused", "after-reconnect")); err != nil {
					_ = stream.Close()
					serverDone <- err
					return
				}
				if err := enc.Encode(workspaceEventMessage("workspace_closed", "after-reconnect")); err != nil {
					_ = stream.Close()
					serverDone <- err
					return
				}
			}
			_ = stream.Close()
		}
		serverDone <- nil
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var got []string
	err := WatchWorkspaceEvents(ctx, socketPath,
		func(id string) error {
			got = append(got, "focus:"+id)
			return nil
		},
		func(id string) error {
			got = append(got, "close:"+id)
			cancel()
			return nil
		},
	)
	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, <-serverDone)
	want := []string{"focus:after-reconnect", "close:after-reconnect"}
	assert.Equal(t, want, got)
}

func TestWatchWorkspaceEventsProtocol20ResyncKeepsAgentJumpSkip(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, state.SaveFocusMRU(dir, state.FocusMRU{
		WorkspaceCurrent: "W1",
		WorkspaceLast:    "W0",
		Migrated:         true,
	}))

	listener, socketPath := listenTestSocket(t)
	jumpArmed := make(chan struct{})
	serverDone := make(chan error, 1)
	go func() {
		stream, err := acceptRequest(listener, "events.subscribe")
		if err != nil {
			serverDone <- err
			return
		}
		enc := json.NewEncoder(stream)
		if err := enc.Encode(map[string]any{"id": "herdr-sesh-history", "result": map[string]any{"type": "subscription_started"}}); err != nil {
			_ = stream.Close()
			serverDone <- err
			return
		}
		if err := serveCompatiblePing(listener, 20); err != nil {
			_ = stream.Close()
			serverDone <- err
			return
		}
		snapshot, err := acceptRequest(listener, "session.snapshot")
		if err != nil {
			_ = stream.Close()
			serverDone <- err
			return
		}
		if err := json.NewEncoder(snapshot).Encode(snapshotResponse("W1", "W1", "W0", "W3")); err != nil {
			_ = snapshot.Close()
			_ = stream.Close()
			serverDone <- err
			return
		}
		_ = snapshot.Close()
		<-jumpArmed
		if err := enc.Encode(workspaceEventMessage("workspace_focused", "W3")); err != nil {
			_ = stream.Close()
			serverDone <- err
			return
		}
		batchSnapshot, err := acceptRequest(listener, "session.snapshot")
		if err != nil {
			_ = stream.Close()
			serverDone <- err
			return
		}
		if err := json.NewEncoder(batchSnapshot).Encode(snapshotResponse("W3", "W1", "W0", "W3")); err != nil {
			_ = batchSnapshot.Close()
			_ = stream.Close()
			serverDone <- err
			return
		}
		_ = batchSnapshot.Close()
		_ = stream.Close()
		serverDone <- nil
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	observed := make(chan string, 8)
	watchDone := make(chan error, 1)
	go func() {
		reconnect, err := watchWorkspaceEventsOnce(ctx, socketPath,
			func(id string) error {
				observed <- id
				return state.Record(dir, id)
			},
			func(id string) error {
				return state.RemoveWorkspace(dir, id)
			},
		)
		_ = listener.Close()
		if reconnect {
			watchDone <- err
			return
		}
		watchDone <- err
	}()

	select {
	case got := <-observed:
		require.Equal(t, "W1", got)
	case <-ctx.Done():
		t.Fatal("watcher never observed the initial snapshot focus")
	}

	m, err := state.LoadFocusMRU(dir)
	require.NoError(t, err)
	assert.Equal(t, "W1", m.WorkspaceCurrent)
	assert.Equal(t, "W0", m.WorkspaceLast)

	// Simulate last-agent jumping to a tab in W3 while the watcher is connected:
	// arm the one-shot suppression, then deliver the workspace.focused W3 event
	// through the protocol-20 batch plus its snapshot reconciliation.
	require.NoError(t, state.PrepareAgentJump(dir, "W3"))
	close(jumpArmed)

	select {
	case <-watchDone:
	case <-ctx.Done():
		t.Fatal("watcher did not finish")
	}
	require.NoError(t, <-serverDone)

	m, err = state.LoadFocusMRU(dir)
	require.NoError(t, err)
	assert.Equal(t, "W1", m.WorkspaceCurrent)
	assert.Equal(t, "W0", m.WorkspaceLast)
	assert.Empty(t, m.SkipWorkspaceID)
	target, ok, err := state.WorkspaceToggleTarget(dir, "W3")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "W0", target)

	// The suppression is one-shot: the next independent real focus change
	// must still update the pair normally.
	require.NoError(t, state.Record(dir, "W4"))
	m, err = state.LoadFocusMRU(dir)
	require.NoError(t, err)
	assert.Equal(t, "W4", m.WorkspaceCurrent)
	assert.Equal(t, "W1", m.WorkspaceLast)
	target, ok, err = state.WorkspaceToggleTarget(dir, "W4")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "W1", target)
}

func listenTestSocket(t *testing.T) (net.Listener, string) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "herdr-sesh-events-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socketPath := filepath.Join(dir, "herdr.sock")
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	return listener, socketPath
}

func acceptRequest(listener net.Listener, method string) (net.Conn, error) {
	conn, got, err := acceptAnyRequest(listener)
	if err != nil {
		return nil, err
	}
	if got != method {
		_ = conn.Close()
		return nil, fmt.Errorf("method=%q want %q", got, method)
	}
	return conn, nil
}

func acceptAnyRequest(listener net.Listener) (net.Conn, string, error) {
	conn, err := listener.Accept()
	if err != nil {
		return nil, "", err
	}
	var request struct {
		Method string `json:"method"`
	}
	if err := json.NewDecoder(conn).Decode(&request); err != nil {
		_ = conn.Close()
		return nil, "", err
	}
	return conn, request.Method, nil
}

func serveCompatiblePing(listener net.Listener, protocol int) error {
	conn, err := acceptRequest(listener, "ping")
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	return encodeCompatiblePing(conn, protocol)
}

func encodeCompatiblePing(conn net.Conn, protocol int) error {
	return json.NewEncoder(conn).Encode(map[string]any{
		"id": "herdr-sesh-history-ping",
		"result": map[string]any{
			"type": "pong", "version": "compatible", "protocol": protocol,
		},
	})
}

func workspaceEventMessage(event, workspaceID string) map[string]any {
	return map[string]any{"event": event, "data": map[string]any{"workspace_id": workspaceID}}
}

func snapshotResponse(focusedWorkspaceID string, workspaceIDs ...string) map[string]any {
	workspaces := make([]map[string]any, 0, len(workspaceIDs))
	for _, workspaceID := range workspaceIDs {
		workspaces = append(workspaces, map[string]any{"workspace_id": workspaceID})
	}
	return map[string]any{
		"id": "herdr-sesh-history-snapshot",
		"result": map[string]any{
			"type": "session_snapshot",
			"snapshot": map[string]any{
				"focused_workspace_id": focusedWorkspaceID,
				"workspaces":           workspaces,
			},
		},
	}
}

func serveProtocol20Snapshots(listener net.Listener, focusedWorkspaceID string, workspaceIDs ...string) error {
	for {
		conn, err := acceptRequest(listener, "session.snapshot")
		if errors.Is(err, net.ErrClosed) {
			return nil
		}
		if err != nil {
			return err
		}
		err = json.NewEncoder(conn).Encode(snapshotResponse(focusedWorkspaceID, workspaceIDs...))
		_ = conn.Close()
		if err != nil {
			return err
		}
	}
}

func recordHistoryVisit(history []string, workspaceID string) []string {
	next := []string{workspaceID}
	for _, existing := range history {
		if existing != workspaceID {
			next = append(next, existing)
		}
	}
	return next
}

func removeHistoryWorkspace(history []string, workspaceID string) []string {
	next := history[:0]
	for _, existing := range history {
		if existing != workspaceID {
			next = append(next, existing)
		}
	}
	return next
}

package mcp

import (
	"testing"
	"time"

	"github.com/netdefense-io/NDCLI/internal/pathfinder"
)

// makeStubSession creates a consoleSession with a nil-safe ExecHandle.
// We can't construct *pathfinder.ExecHandle directly (unexported fields),
// but Close() guards against nil pointers so a zero-value handle is safe
// as long as we don't call Exec on it.
func makeStubSession(id, org, device string) *consoleSession {
	now := time.Now()
	return &consoleSession{
		sessionID:    id,
		org:          org,
		device:       device,
		openedAt:     now,
		lastActivity: now,
		handle:       &pathfinder.ExecHandle{},
	}
}

// TestConsoleSessionManager_AddGetRemove verifies the basic lifecycle: add a
// session, retrieve it, remove it, and confirm it is gone.
func TestConsoleSessionManager_AddGetRemove(t *testing.T) {
	m := newConsoleSessionManager()
	defer m.CloseAll()

	sess := makeStubSession("sess-1", "acme", "fw-01")
	m.Add(sess)

	got, err := m.Get("sess-1")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got.device != "fw-01" {
		t.Errorf("device = %q, want %q", got.device, "fw-01")
	}

	removed := m.Remove("sess-1")
	if !removed {
		t.Error("Remove returned false, want true")
	}

	_, err = m.Get("sess-1")
	if err == nil {
		t.Error("Get after remove returned nil error, want SESSION_NOT_FOUND")
	}
}

// TestConsoleSessionManager_GetNotFound verifies that looking up a missing
// session returns a SESSION_NOT_FOUND ToolError.
func TestConsoleSessionManager_GetNotFound(t *testing.T) {
	m := newConsoleSessionManager()
	defer m.CloseAll()

	_, err := m.Get("does-not-exist")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	te, ok := err.(*ToolError)
	if !ok {
		t.Fatalf("expected *ToolError, got %T: %v", err, err)
	}
	if te.Code != "SESSION_NOT_FOUND" {
		t.Errorf("code = %q, want SESSION_NOT_FOUND", te.Code)
	}
}

// TestConsoleSessionManager_RemoveIdempotent verifies that removing a session
// twice returns false the second time without panicking.
func TestConsoleSessionManager_RemoveIdempotent(t *testing.T) {
	m := newConsoleSessionManager()
	defer m.CloseAll()

	m.Add(makeStubSession("sess-2", "acme", "fw-02"))

	if !m.Remove("sess-2") {
		t.Error("first Remove returned false, want true")
	}
	if m.Remove("sess-2") {
		t.Error("second Remove returned true, want false (idempotent)")
	}
}

// TestConsoleSessionManager_List verifies that List returns all active sessions.
func TestConsoleSessionManager_List(t *testing.T) {
	m := newConsoleSessionManager()
	defer m.CloseAll()

	for _, id := range []string{"a", "b", "c"} {
		m.Add(makeStubSession(id, "acme", "dev-"+id))
	}

	sessions := m.List()
	if len(sessions) != 3 {
		t.Errorf("List returned %d sessions, want 3", len(sessions))
	}
}

// TestConsoleSessionManager_ListEmpty verifies List on an empty manager.
func TestConsoleSessionManager_ListEmpty(t *testing.T) {
	m := newConsoleSessionManager()
	defer m.CloseAll()

	sessions := m.List()
	if len(sessions) != 0 {
		t.Errorf("List returned %d sessions, want 0", len(sessions))
	}
}

// TestConsoleSessionManager_IdleReaper verifies that the reaper closes
// sessions that have been idle past the threshold.
func TestConsoleSessionManager_IdleReaper(t *testing.T) {
	m := newConsoleSessionManager()
	defer m.CloseAll()

	// Create a session that is already past the idle threshold.
	old := makeStubSession("old-session", "acme", "fw-old")
	old.lastActivity = time.Now().Add(-consoleSessionIdleTimeout - time.Minute)
	m.Add(old)

	// Create a fresh session.
	fresh := makeStubSession("new-session", "acme", "fw-new")
	m.Add(fresh)

	// Trigger reap manually.
	m.reapIdle()

	_, err := m.Get("old-session")
	if err == nil {
		t.Error("idle session should have been reaped, but Get still returned it")
	}

	_, err = m.Get("new-session")
	if err != nil {
		t.Errorf("fresh session should still exist: %v", err)
	}
}

// TestConsoleSessionManager_CloseAll verifies that CloseAll removes all
// sessions and stops the reaper without panicking.
func TestConsoleSessionManager_CloseAll(t *testing.T) {
	m := newConsoleSessionManager()

	for _, id := range []string{"x", "y", "z"} {
		m.Add(makeStubSession(id, "acme", "dev-"+id))
	}

	m.CloseAll()

	sessions := m.List()
	if len(sessions) != 0 {
		t.Errorf("List after CloseAll returned %d sessions, want 0", len(sessions))
	}
}

// TestNewConsoleSessionID verifies that session IDs have UUID v4 format.
func TestNewConsoleSessionID(t *testing.T) {
	id, err := newConsoleSessionID()
	if err != nil {
		t.Fatalf("newConsoleSessionID error: %v", err)
	}
	if len(id) != 36 {
		t.Errorf("ID length = %d, want 36", len(id))
	}
	// Check hyphen positions.
	for _, pos := range []int{8, 13, 18, 23} {
		if id[pos] != '-' {
			t.Errorf("expected '-' at position %d, got %c", pos, id[pos])
		}
	}
}

// TestConsoleSession_UpdateActivity verifies that updateActivity advances
// the lastActivity timestamp.
func TestConsoleSession_UpdateActivity(t *testing.T) {
	sess := makeStubSession("s", "org", "dev")
	before := sess.lastActivity
	time.Sleep(time.Millisecond)
	sess.updateActivity()
	if !sess.lastActivity.After(before) {
		t.Error("lastActivity did not advance after updateActivity")
	}
}

package mcp

import (
	"fmt"
	"sync"
	"time"

	"github.com/netdefense-io/NDCLI/internal/pathfinder"
)

const (
	// consoleSessionIdleTimeout is how long a session may be idle before the
	// background reaper closes it to prevent relay leaks.
	consoleSessionIdleTimeout = 30 * time.Minute

	// consoleReaperInterval is how often the background reaper wakes up to
	// scan for idle sessions.
	consoleReaperInterval = 5 * time.Minute
)

// consoleSession holds a single live exec-stream connection to a device.
// The mu field serialises Exec calls so the exec stream never has two
// in-flight commands simultaneously (wire-contract requirement).
type consoleSession struct {
	sessionID string
	org       string
	device    string
	openedAt  time.Time

	handle *pathfinder.ExecHandle

	// mu serialises Exec calls for this session.
	mu sync.Mutex

	// actMu guards lastActivity independently of mu (and of the manager's
	// map lock) so the idle reaper and console_list can read the timestamp
	// without blocking on, or racing with, an in-flight Exec call.
	actMu        sync.Mutex
	lastActivity time.Time
}

// updateActivity records the current time as the session's last-activity
// timestamp. Safe to call concurrently with reapIdle / getLastActivity —
// guarded by actMu, independent of the Exec-serialising mu.
func (s *consoleSession) updateActivity() {
	s.actMu.Lock()
	s.lastActivity = time.Now()
	s.actMu.Unlock()
}

// getLastActivity returns the session's last-activity timestamp, guarded by
// actMu so it never races with a concurrent updateActivity.
func (s *consoleSession) getLastActivity() time.Time {
	s.actMu.Lock()
	defer s.actMu.Unlock()
	return s.lastActivity
}

// ConsoleSessionManager keeps all live console sessions for the lifetime of
// the MCP server process. It is attached to the Server struct and its Close
// method is called on MCP shutdown.
type ConsoleSessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*consoleSession
	done     chan struct{}
}

// newConsoleSessionManager creates and starts the manager, including the
// background idle reaper goroutine.
func newConsoleSessionManager() *ConsoleSessionManager {
	m := &ConsoleSessionManager{
		sessions: make(map[string]*consoleSession),
		done:     make(chan struct{}),
	}
	go m.reaperLoop()
	return m
}

// Add registers a new session. The sessionID must be unique (caller generates
// a UUID before calling).
func (m *ConsoleSessionManager) Add(sess *consoleSession) {
	m.mu.Lock()
	m.sessions[sess.sessionID] = sess
	m.mu.Unlock()
}

// Get retrieves a session by ID. Returns (nil, error) if not found.
func (m *ConsoleSessionManager) Get(sessionID string) (*consoleSession, error) {
	m.mu.RLock()
	sess := m.sessions[sessionID]
	m.mu.RUnlock()
	if sess == nil {
		return nil, &ToolError{
			Code:    "SESSION_NOT_FOUND",
			Message: fmt.Sprintf("console session %q not found (may have expired or been closed)", sessionID),
		}
	}
	return sess, nil
}

// Remove closes and removes a session by ID. Idempotent: returns false if the
// session was already removed.
func (m *ConsoleSessionManager) Remove(sessionID string) bool {
	m.mu.Lock()
	sess := m.sessions[sessionID]
	if sess != nil {
		delete(m.sessions, sessionID)
	}
	m.mu.Unlock()

	if sess == nil {
		return false
	}
	sess.handle.Close()
	return true
}

// List returns a snapshot of all active sessions ordered by open time
// (implementation returns map iteration order, which is randomised; the
// caller may sort if needed).
func (m *ConsoleSessionManager) List() []*consoleSession {
	m.mu.RLock()
	out := make([]*consoleSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s)
	}
	m.mu.RUnlock()
	return out
}

// CloseAll closes all active sessions. Called on MCP server shutdown.
func (m *ConsoleSessionManager) CloseAll() {
	close(m.done) // stop the reaper

	m.mu.Lock()
	sessions := m.sessions
	m.sessions = make(map[string]*consoleSession)
	m.mu.Unlock()

	for _, sess := range sessions {
		sess.handle.Close()
	}
}

// reaperLoop wakes up every consoleReaperInterval and closes sessions that
// have been idle for longer than consoleSessionIdleTimeout.
func (m *ConsoleSessionManager) reaperLoop() {
	ticker := time.NewTicker(consoleReaperInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.done:
			return
		case <-ticker.C:
			m.reapIdle()
		}
	}
}

func (m *ConsoleSessionManager) reapIdle() {
	cutoff := time.Now().Add(-consoleSessionIdleTimeout)

	// Collect idle IDs under a read lock first to minimise lock contention.
	m.mu.RLock()
	var idle []string
	for id, sess := range m.sessions {
		if sess.getLastActivity().Before(cutoff) {
			idle = append(idle, id)
		}
	}
	m.mu.RUnlock()

	for _, id := range idle {
		m.Remove(id)
	}
}

package memory

import (
	"encoding/json"
	"sync"
	"time"
)

// SessionMemory maintains context across multiple tool calls for an agent
type SessionMemory struct {
	mu         sync.RWMutex
	sessions   map[string]*Session
	maxEntries int
	ttl        time.Duration
}

// Session represents a single agent's session memory
type Session struct {
	AgentID    string                 `json:"agent_id"`
	CreatedAt  time.Time              `json:"created_at"`
	LastAccess time.Time              `json:"last_access"`
	Context    map[string]interface{} `json:"context"`
	History    []MemoryEntry          `json:"history"`
}

// MemoryEntry represents a single tool call in history
type MemoryEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	ToolName  string                 `json:"tool_name"`
	Inputs    map[string]interface{} `json:"inputs"`
	Output    string                 `json:"output"`
	Success   bool                   `json:"success"`
}

// Config for session memory
type Config struct {
	MaxEntries int           `json:"max_entries"` // Max entries per session
	TTL        time.Duration `json:"ttl"`         // Session time-to-live
}

// New creates a new session memory manager
func New(cfg *Config) *SessionMemory {
	if cfg == nil {
		cfg = &Config{
			MaxEntries: 100,
			TTL:        30 * time.Minute,
		}
	}
	return &SessionMemory{
		sessions:   make(map[string]*Session),
		maxEntries: cfg.MaxEntries,
		ttl:        cfg.TTL,
	}
}

// GetOrCreateSession retrieves or creates a session for an agent
func (sm *SessionMemory) GetOrCreateSession(agentID string) *Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[agentID]
	if !exists {
		session = &Session{
			AgentID:    agentID,
			CreatedAt:  time.Now(),
			LastAccess: time.Now(),
			Context:    make(map[string]interface{}),
			History:    make([]MemoryEntry, 0, sm.maxEntries),
		}
		sm.sessions[agentID] = session
	} else {
		session.LastAccess = time.Now()
	}

	return session
}

// SetContext stores context data for an agent session
func (sm *SessionMemory) SetContext(agentID, key string, value interface{}) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session := sm.getOrCreateSessionLocked(agentID)
	session.Context[key] = value
	session.LastAccess = time.Now()
}

// GetContext retrieves context data for an agent session
func (sm *SessionMemory) GetContext(agentID, key string) (interface{}, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[agentID]
	if !exists {
		return nil, false
	}

	value, exists := session.Context[key]
	return value, exists
}

// GetAllContext retrieves all context data for an agent session
func (sm *SessionMemory) GetAllContext(agentID string) map[string]interface{} {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[agentID]
	if !exists {
		return make(map[string]interface{})
	}

	// Return a copy to avoid race conditions
	result := make(map[string]interface{}, len(session.Context))
	for k, v := range session.Context {
		result[k] = v
	}
	return result
}

// AddHistory adds a tool call to the session history
func (sm *SessionMemory) AddHistory(agentID, toolName string, inputs map[string]interface{}, output string, success bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session := sm.getOrCreateSessionLocked(agentID)
	entry := MemoryEntry{
		Timestamp: time.Now(),
		ToolName:  toolName,
		Inputs:    inputs,
		Output:    output,
		Success:   success,
	}

	session.History = append(session.History, entry)
	session.LastAccess = time.Now()

	// Trim history if it exceeds max entries
	if len(session.History) > sm.maxEntries {
		session.History = session.History[len(session.History)-sm.maxEntries:]
	}
}

// GetHistory retrieves the history for an agent session
func (sm *SessionMemory) GetHistory(agentID string) []MemoryEntry {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[agentID]
	if !exists {
		return []MemoryEntry{}
	}

	// Return a copy to avoid race conditions
	history := make([]MemoryEntry, len(session.History))
	copy(history, session.History)
	return history
}

// ClearHistory clears the history for an agent session
func (sm *SessionMemory) ClearHistory(agentID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[agentID]
	if exists {
		session.History = make([]MemoryEntry, 0, sm.maxEntries)
		session.LastAccess = time.Now()
	}
}

// ClearContext clears all context for an agent session
func (sm *SessionMemory) ClearContext(agentID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[agentID]
	if exists {
		session.Context = make(map[string]interface{})
		session.LastAccess = time.Now()
	}
}

// DeleteSession deletes an agent's session
func (sm *SessionMemory) DeleteSession(agentID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	delete(sm.sessions, agentID)
}

// GetSession retrieves a session (read-only)
func (sm *SessionMemory) GetSession(agentID string) (*Session, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[agentID]
	if !exists {
		return nil, false
	}

	// Return a copy
	copySession := &Session{
		AgentID:    session.AgentID,
		CreatedAt:  session.CreatedAt,
		LastAccess: session.LastAccess,
		Context:    make(map[string]interface{}),
		History:    make([]MemoryEntry, len(session.History)),
	}
	for k, v := range session.Context {
		copySession.Context[k] = v
	}
	copy(copySession.History, session.History)

	return copySession, true
}

// GetAllSessions retrieves all active sessions
func (sm *SessionMemory) GetAllSessions() map[string]*Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	result := make(map[string]*Session, len(sm.sessions))
	for id, session := range sm.sessions {
		copySession := &Session{
			AgentID:    session.AgentID,
			CreatedAt:  session.CreatedAt,
			LastAccess: session.LastAccess,
			Context:    make(map[string]interface{}),
			History:    make([]MemoryEntry, len(session.History)),
		}
		for k, v := range session.Context {
			copySession.Context[k] = v
		}
		copy(copySession.History, session.History)
		result[id] = copySession
	}
	return result
}

// CleanupExpiredSessions removes sessions that have exceeded their TTL
func (sm *SessionMemory) CleanupExpiredSessions() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now()
	deleted := 0

	for agentID, session := range sm.sessions {
		if now.Sub(session.LastAccess) > sm.ttl {
			delete(sm.sessions, agentID)
			deleted++
		}
	}

	return deleted
}

// StartCleanupRoutine starts a background routine to clean up expired sessions
func (sm *SessionMemory) StartCleanupRoutine(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			deleted := sm.CleanupExpiredSessions()
			if deleted > 0 {
				// Log cleanup if needed
			}
		}
	}()
}

// getOrCreateSessionLocked is a helper that assumes lock is held
func (sm *SessionMemory) getOrCreateSessionLocked(agentID string) *Session {
	session, exists := sm.sessions[agentID]
	if !exists {
		session = &Session{
			AgentID:    agentID,
			CreatedAt:  time.Now(),
			LastAccess: time.Now(),
			Context:    make(map[string]interface{}),
			History:    make([]MemoryEntry, 0, sm.maxEntries),
		}
		sm.sessions[agentID] = session
	} else {
		session.LastAccess = time.Now()
	}
	return session
}

// ExportSession exports a session as JSON
func (sm *SessionMemory) ExportSession(agentID string) (string, error) {
	session, exists := sm.GetSession(agentID)
	if !exists {
		return "", nil
	}

	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ImportSession imports a session from JSON
func (sm *SessionMemory) ImportSession(jsonData string) error {
	var session Session
	if err := json.Unmarshal([]byte(jsonData), &session); err != nil {
		return err
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.sessions[session.AgentID] = &session
	return nil
}

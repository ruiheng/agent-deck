package session

import (
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/statedb"
)

const (
	toolDataAgySessionIDKey  = "agy_session_id"
	toolDataAgyDetectedAtKey = "agy_detected_at"
)

// WriteAgySessionIDToToolData merges the entry's Antigravity conversation ID
// into tool_data. An empty ID omits the key unless cleared is true, so
// MergeToolDataExtras preserves the on-disk value when a writer has not observed
// the binding; an explicit clear writes empty/zero so the clear sticks.
func WriteAgySessionIDToToolData(td json.RawMessage, sessionID string, detectedAt time.Time, cleared bool) json.RawMessage {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" && !cleared {
		return td
	}
	m := map[string]json.RawMessage{}
	if len(td) > 0 {
		_ = json.Unmarshal(td, &m)
	}
	if sessionID == "" {
		m[toolDataAgySessionIDKey] = json.RawMessage(`""`)
		m[toolDataAgyDetectedAtKey] = json.RawMessage("0")
	} else {
		rawID, err := json.Marshal(sessionID)
		if err != nil {
			return td
		}
		m[toolDataAgySessionIDKey] = rawID
		if !detectedAt.IsZero() {
			if rawAt, err := json.Marshal(detectedAt.Unix()); err == nil {
				m[toolDataAgyDetectedAtKey] = rawAt
			}
		}
	}
	out, err := json.Marshal(m)
	if err != nil {
		return td
	}
	return out
}

// ReadAgySessionIDFromToolData returns the recorded Antigravity conversation ID, or "".
func ReadAgySessionIDFromToolData(td json.RawMessage) string {
	if len(td) == 0 {
		return ""
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(td, &m); err != nil {
		return ""
	}
	var id string
	if err := json.Unmarshal(m[toolDataAgySessionIDKey], &id); err != nil {
		return ""
	}
	return strings.TrimSpace(id)
}

// ReadAgyDetectedAtFromToolData returns when the agy binding was recorded, or zero.
func ReadAgyDetectedAtFromToolData(td json.RawMessage) time.Time {
	if len(td) == 0 {
		return time.Time{}
	}
	var blob struct {
		At int64 `json:"agy_detected_at"`
	}
	if json.Unmarshal(td, &blob) != nil || blob.At <= 0 {
		return time.Time{}
	}
	return time.Unix(blob.At, 0).UTC()
}

// PersistAgySessionBinding writes the instance's AgySessionID and AgyDetectedAt
// directly to SQLite (matching PersistGenericSessionBinding for CLI `session set`).
func PersistAgySessionBinding(db *statedb.StateDB, inst *Instance) error {
	if db == nil || inst == nil {
		return nil
	}
	return db.WriteAgySessionBinding(inst.ID, inst.AgySessionID, inst.AgyDetectedAt)
}

func (i *Instance) persistAgySessionBinding(sessionID string, detectedAt time.Time) {
	err := i.withOwningStateDB(func(db *statedb.StateDB) error {
		return db.WriteAgySessionBinding(i.ID, sessionID, detectedAt)
	})
	if errors.Is(err, errNoStateDB) {
		return
	}
	if err != nil {
		sessionLog.Warn("agy_session_bind_persist_failed",
			slog.String("instance_id", i.ID),
			slog.String("new_id", sessionID),
			slog.String("error", err.Error()))
	}
}

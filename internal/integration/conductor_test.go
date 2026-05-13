package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/session"
	"github.com/asheshgoplani/agent-deck/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConductor_SendToChild verifies that a child session running `cat` receives
// text sent via SendKeysAndEnter and the text appears in the child's pane content. (COND-01)
func TestConductor_SendToChild(t *testing.T) {
	skipWindowsPsmuxStress(t)
	h := NewTmuxHarness(t)

	inst := h.CreateSession("cond-child", testWorkDir(t))
	require.NoError(t, inst.Start())

	WaitForCondition(t, 5*time.Second, 200*time.Millisecond,
		"session to exist",
		func() bool { return inst.Exists() })

	tmuxSess := inst.GetTmuxSession()
	require.NotNil(t, tmuxSess, "tmux session should not be nil")

	msg := "hello-from-conductor-" + t.Name()
	require.NoError(t, tmuxSess.SendKeysAndEnter(shellEchoCommand(msg)))

	WaitForPaneContent(t, inst, "hello-from-conductor-", 5*time.Second)
}

// TestConductor_SendMultipleMessages verifies that two sequential messages sent
// via SendKeysAndEnter both appear in the child's pane content, proving reliable
// sequential delivery. (COND-01)
func TestConductor_SendMultipleMessages(t *testing.T) {
	skipWindowsPsmuxStress(t)
	h := NewTmuxHarness(t)

	inst := h.CreateSession("cond-multi", testWorkDir(t))
	require.NoError(t, inst.Start())

	WaitForCondition(t, 5*time.Second, 200*time.Millisecond,
		"session to exist",
		func() bool { return inst.Exists() })

	tmuxSess := inst.GetTmuxSession()
	require.NotNil(t, tmuxSess, "tmux session should not be nil")

	require.NoError(t, tmuxSess.SendKeysAndEnter(shellEchoCommand("msg-one")))
	WaitForPaneContent(t, inst, "msg-one", 5*time.Second)

	require.NoError(t, tmuxSess.SendKeysAndEnter(shellEchoCommand("msg-two")))
	WaitForPaneContent(t, inst, "msg-two", 5*time.Second)
}

// TestConductor_EventWriteWatch verifies that a StatusEvent written via WriteStatusEvent
// is detected by StatusEventWatcher.WaitForStatus and delivered with matching fields. (COND-02)
func TestConductor_EventWriteWatch(t *testing.T) {
	cleanup := testutil.IsolateHome("agent-deck-integration-event-")
	defer cleanup()

	instanceID := fmt.Sprintf("inttest-event-%d", time.Now().UnixNano())

	// Clean up event file after test.
	t.Cleanup(func() {
		_ = os.Remove(filepath.Join(session.GetEventsDir(), instanceID+".json"))
	})

	watcher, err := session.NewStatusEventWatcher(instanceID)
	require.NoError(t, err)
	defer watcher.Stop()

	go watcher.Start()

	// Allow time for fsnotify to register the watch (100ms debounce + startup).
	time.Sleep(300 * time.Millisecond)

	event := session.StatusEvent{
		InstanceID: instanceID,
		Title:      "test-child",
		Tool:       "claude",
		Status:     "waiting",
		PrevStatus: "running",
		Timestamp:  time.Now().Unix(),
	}
	require.NoError(t, session.WriteStatusEvent(event))

	received, err := watcher.WaitForStatus([]string{"waiting"}, 5*time.Second)
	require.NoError(t, err)

	assert.Equal(t, instanceID, received.InstanceID, "instance ID should match")
	assert.Equal(t, "waiting", received.Status, "status should match")
	assert.Equal(t, "running", received.PrevStatus, "prev status should match")
}

// TestConductor_EventWatcherFilters verifies that a watcher filtering for instance "A"
// does NOT receive events for instance "B", but DOES receive events for instance "A". (COND-02)
func TestConductor_EventWatcherFilters(t *testing.T) {
	cleanup := testutil.IsolateHome("agent-deck-integration-event-")
	defer cleanup()

	idA := fmt.Sprintf("inttest-filter-a-%d", time.Now().UnixNano())
	idB := fmt.Sprintf("inttest-filter-b-%d", time.Now().UnixNano())

	// Clean up event files after test.
	t.Cleanup(func() {
		_ = os.Remove(filepath.Join(session.GetEventsDir(), idA+".json"))
		_ = os.Remove(filepath.Join(session.GetEventsDir(), idB+".json"))
	})

	watcher, err := session.NewStatusEventWatcher(idA)
	require.NoError(t, err)
	defer watcher.Stop()

	go watcher.Start()

	// Allow time for fsnotify to register the watch.
	time.Sleep(300 * time.Millisecond)

	// Write event for idB first (should be filtered out by the watcher).
	eventB := session.StatusEvent{
		InstanceID: idB,
		Title:      "child-b",
		Tool:       "claude",
		Status:     "waiting",
		PrevStatus: "running",
		Timestamp:  time.Now().Unix(),
	}
	require.NoError(t, session.WriteStatusEvent(eventB))

	// Write event for idA second.
	eventA := session.StatusEvent{
		InstanceID: idA,
		Title:      "child-a",
		Tool:       "claude",
		Status:     "waiting",
		PrevStatus: "idle",
		Timestamp:  time.Now().Unix(),
	}
	require.NoError(t, session.WriteStatusEvent(eventA))

	received, err := watcher.WaitForStatus([]string{"waiting"}, 5*time.Second)
	require.NoError(t, err)

	assert.Equal(t, idA, received.InstanceID, "should receive event for filtered instance A, not B")
}

// TestConductor_HeartbeatRoundTrip verifies the heartbeat pipeline: parent checks
// child existence, sends a heartbeat-prefixed message, and confirms receipt in the
// child's pane content. Mirrors the production heartbeat script logic. (COND-03)
func TestConductor_HeartbeatRoundTrip(t *testing.T) {
	skipWindowsPsmuxStress(t)
	h := NewTmuxHarness(t)

	inst := h.CreateSession("cond-heartbeat", testWorkDir(t))
	require.NoError(t, inst.Start())

	// Wait for child session to exist (heartbeat first checks session status).
	WaitForCondition(t, 5*time.Second, 200*time.Millisecond,
		"session to exist",
		func() bool { return inst.Exists() })

	// Verify existence explicitly (mirrors heartbeat's "check status" step).
	require.True(t, inst.Exists(), "child session must exist before heartbeat send")

	tmuxSess := inst.GetTmuxSession()
	require.NotNil(t, tmuxSess, "tmux session should not be nil")

	// Send heartbeat message (cat echoes it back to pane).
	heartbeatMsg := "HEARTBEAT: check-all-sessions-" + t.Name()
	require.NoError(t, tmuxSess.SendKeysAndEnter(shellEchoCommand(heartbeatMsg)))

	// Verify receipt via polling.
	WaitForPaneContent(t, inst, "HEARTBEAT:", 5*time.Second)

	// Additionally capture and assert the full heartbeat text is present.
	content, err := tmuxSess.CapturePaneFresh()
	require.NoError(t, err)
	assert.Contains(t, content, heartbeatMsg, "pane should contain the full heartbeat message")
}

// TestConductor_ChunkedSendDelivery verifies that a message exceeding the 4096-byte
// tmux chunk threshold is delivered intact via SendKeysChunked. The large payload is
// split into multiple chunks with 50ms inter-chunk delay. (COND-04)
//
// The payload uses embedded newlines so that each chunk is flushed through the
// terminal line discipline independently (canonical mode buffers up to ~4096 bytes
// per line). A final sentinel line verifies end-to-end sequential delivery.
func TestConductor_ChunkedSendDelivery(t *testing.T) {
	skipWindowsPsmuxStress(t)
	h := NewTmuxHarness(t)

	inst := h.CreateSession("cond-chunked", testWorkDir(t))
	require.NoError(t, inst.Start())

	WaitForCondition(t, 5*time.Second, 200*time.Millisecond,
		"session to exist",
		func() bool { return inst.Exists() })

	tmuxSess := inst.GetTmuxSession()
	require.NotNil(t, tmuxSess, "tmux session should not be nil")

	bigMsg := chunkedMarkerCommand("CHUNK-END")
	require.Greater(t, len(bigMsg), 4096, "message must exceed chunk threshold")

	// Use SendKeysChunked directly to test the chunking path in isolation.
	// The embedded newlines cause cat to echo each line as it receives it.
	require.NoError(t, tmuxSess.SendKeysChunked(bigMsg))
	require.NoError(t, tmuxSess.SendEnter())

	// Use longer timeout since chunked sending incurs 50ms inter-chunk delays.
	// Verify the last line (CHUNK-END) was delivered, proving no truncation.
	WaitForPaneContent(t, inst, "CHUNK-END", 30*time.Second)

	content, err := tmuxSess.CapturePaneFresh()
	require.NoError(t, err)
	assert.Contains(t, content, "CHUNK-END", "pane should contain end marker, proving full chunked delivery")
}

// TestConductor_SmallSendDelivery verifies that a message below the 4096-byte
// threshold is delivered via the non-chunked (single SendKeys) path. (COND-04)
func TestConductor_SmallSendDelivery(t *testing.T) {
	skipWindowsPsmuxStress(t)
	h := NewTmuxHarness(t)

	inst := h.CreateSession("cond-small", testWorkDir(t))
	require.NoError(t, inst.Start())

	WaitForCondition(t, 5*time.Second, 200*time.Millisecond,
		"session to exist",
		func() bool { return inst.Exists() })

	tmuxSess := inst.GetTmuxSession()
	require.NotNil(t, tmuxSess, "tmux session should not be nil")

	// Build a small message that stays under the 4096-byte threshold.
	smallMsg := shellEchoCommand("SMALL-MSG-" + strings.Repeat("A", 100) + "-END")
	require.Less(t, len(smallMsg), 4096, "message must be under chunk threshold")

	// SendKeysChunked with small content delegates to SendKeys (no chunking).
	require.NoError(t, tmuxSess.SendKeysChunked(smallMsg))
	require.NoError(t, tmuxSess.SendEnter())

	WaitForPaneContent(t, inst, "SMALL-MSG-", 5*time.Second)

	content, err := tmuxSess.CapturePaneFresh()
	require.NoError(t, err)
	assert.Contains(t, content, "-END", "pane should contain end marker")
}

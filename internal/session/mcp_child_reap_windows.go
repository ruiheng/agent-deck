//go:build windows

package session

import (
	"log/slog"
	"os"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/processutil"
)

const mcpReapGracePeriod = 1 * time.Second

const mcpReapVerifyTimeout = 2 * time.Second

// RegisterMCPChild records the OS PID of a stdio MCP child spawned for
// this session. Safe to call concurrently. Passing pid <= 0 is a no-op.
func (i *Instance) RegisterMCPChild(pid int) {
	if pid <= 0 {
		return
	}
	i.mcpPIDsMu.Lock()
	defer i.mcpPIDsMu.Unlock()
	for _, existing := range i.TrackedMCPPIDs {
		if existing == pid {
			return
		}
	}
	i.TrackedMCPPIDs = append(i.TrackedMCPPIDs, pid)
}

// UnregisterMCPChild removes a previously registered MCP child PID.
func (i *Instance) UnregisterMCPChild(pid int) {
	if pid <= 0 {
		return
	}
	i.mcpPIDsMu.Lock()
	defer i.mcpPIDsMu.Unlock()
	out := i.TrackedMCPPIDs[:0]
	for _, p := range i.TrackedMCPPIDs {
		if p != pid {
			out = append(out, p)
		}
	}
	i.TrackedMCPPIDs = out
}

// discoverMCPChildrenFromPaneTree is Unix-only for now. Native Windows psmux
// does not expose the same ps-compatible pane process tree used by the Unix
// implementation.
func (i *Instance) discoverMCPChildrenFromPaneTree() {
}

func (i *Instance) readPanePID() int {
	return 0
}

func (i *Instance) reapTrackedMCPChildren() {
	i.mcpPIDsMu.Lock()
	pids := append([]int(nil), i.TrackedMCPPIDs...)
	i.TrackedMCPPIDs = nil
	i.mcpPIDsMu.Unlock()

	for _, pid := range pids {
		proc, err := os.FindProcess(pid)
		if err != nil {
			continue
		}
		if err := processutil.TerminateProcessTree(proc); err != nil {
			mcpLog.Debug("mcp_child_terminate_failed", slog.Int("pid", pid), slog.Any("error", err))
		}
	}
	if waitPIDsGone(pids, mcpReapGracePeriod) {
		return
	}

	for _, pid := range pids {
		proc, err := os.FindProcess(pid)
		if err != nil {
			continue
		}
		if err := processutil.KillProcessTree(proc); err != nil {
			mcpLog.Debug("mcp_child_kill_failed", slog.Int("pid", pid), slog.Any("error", err))
		}
	}
	if !waitPIDsGone(pids, mcpReapVerifyTimeout) {
		mcpLog.Warn("mcp_child_kill_unverified",
			slog.Any("pids", pids),
			slog.Duration("waited", mcpReapVerifyTimeout))
	}
}

func waitPIDsGone(pids []int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for {
		anyAlive := false
		for _, pid := range pids {
			proc, err := os.FindProcess(pid)
			if err == nil && processutil.CheckProcessRunning(proc) == nil {
				anyAlive = true
				break
			}
		}
		if !anyAlive {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

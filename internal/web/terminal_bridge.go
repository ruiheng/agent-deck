package web

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/processutil"
	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

var ErrTmuxSessionNotFound = errors.New("tmux session not found")

type wsConnWriter struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func newWSConnWriter(conn *websocket.Conn) *wsConnWriter {
	return &wsConnWriter{conn: conn}
}

func (w *wsConnWriter) WriteJSON(v any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return w.conn.WriteJSON(v)
}

func (w *wsConnWriter) WriteBinary(data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return w.conn.WriteMessage(websocket.BinaryMessage, data)
}

type terminalWriter interface {
	WriteJSON(v any) error
	WriteBinary(data []byte) error
}

type tmuxPTYBridge struct {
	tmuxSession    string
	tmuxSocketName string // tmux -L selector captured from Instance (issue #687)
	sessionID      string
	writer         terminalWriter

	cmd *exec.Cmd

	// ptmxMu guards ptmx against a concurrent Close/Resize race. Close
	// closes the PTY file and nils the pointer under the write lock;
	// Resize reads under the read lock so Setsize cannot hit a freshly
	// closed fd. Observed as an intermittent TestTmuxPTYBridgeResize
	// -race failure on CI (v1.7.4, v1.7.5 release workflows).
	ptmxMu sync.RWMutex
	ptmx   *os.File

	closeOnce sync.Once
	done      chan struct{}

	polling  bool
	pollStop chan struct{}
	pollMu   sync.Mutex
	pollLast string
}

func newTmuxPTYBridge(tmuxSession, tmuxSocketName, sessionID string, writer *wsConnWriter) (*tmuxPTYBridge, error) {
	if tmuxSession == "" {
		return nil, fmt.Errorf("tmux session name is required")
	}
	if writer == nil {
		return nil, fmt.Errorf("writer is required")
	}
	exists, err := tmuxSessionExists(tmuxSession, tmuxSocketName)
	if err != nil {
		return nil, fmt.Errorf("check tmux session %q: %w", tmuxSession, err)
	}
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrTmuxSessionNotFound, tmuxSession)
	}

	if runtime.GOOS == "windows" {
		b := &tmuxPTYBridge{
			tmuxSession:    tmuxSession,
			tmuxSocketName: tmuxSocketName,
			sessionID:      sessionID,
			writer:         writer,
			done:           make(chan struct{}),
			polling:        true,
			pollStop:       make(chan struct{}),
		}
		go b.pollOutput()
		return b, nil
	}

	cmd := tmuxAttachCommand(tmuxSession, tmuxSocketName)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("start tmux pty: %w", err)
	}

	b := &tmuxPTYBridge{
		tmuxSession:    tmuxSession,
		tmuxSocketName: tmuxSocketName,
		sessionID:      sessionID,
		writer:         writer,
		cmd:            cmd,
		ptmx:           ptmx,
		done:           make(chan struct{}),
	}

	go b.streamOutput()
	return b, nil
}

func (b *tmuxPTYBridge) pollOutput() {
	defer close(b.done)

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	_ = b.captureAndWriteIfChanged(true)
	for {
		select {
		case <-b.pollStop:
			return
		case <-ticker.C:
			if err := b.captureAndWriteIfChanged(false); err != nil {
				_ = b.writer.WriteJSON(wsServerMessage{
					Type:      "status",
					Event:     "session_closed",
					SessionID: b.sessionID,
					Time:      time.Now().UTC(),
				})
				b.Close()
				return
			}
		}
	}
}

func (b *tmuxPTYBridge) captureAndWriteIfChanged(force bool) error {
	b.pollMu.Lock()
	defer b.pollMu.Unlock()

	output, err := b.capturePaneOutput()
	if err != nil {
		return err
	}
	content := string(output)
	cursorX, cursorY, hasCursor := b.cursorPosition()
	signature := content
	if hasCursor {
		signature = fmt.Sprintf("%s\x00%d,%d", content, cursorX, cursorY)
	}
	if !force && signature == b.pollLast {
		return nil
	}
	b.pollLast = signature
	return b.writer.WriteBinary([]byte(formatPollingCaptureForTerminal(content, cursorX, cursorY, hasCursor)))
}

func (b *tmuxPTYBridge) capturePaneOutput() ([]byte, error) {
	args := []string{"capture-pane", "-t", b.tmuxSession, "-p", "-e"}
	if b.alternateScreenActive() {
		if output, err := tmuxCommand(b.tmuxSocketName, append(args, "-a")...).Output(); err == nil {
			return output, nil
		}
	}
	return tmuxCommand(b.tmuxSocketName, args...).Output()
}

func (b *tmuxPTYBridge) alternateScreenActive() bool {
	output, err := tmuxCommand(b.tmuxSocketName, "display-message", "-p", "-t", b.tmuxSession, "#{alternate_on}").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "1"
}

func (b *tmuxPTYBridge) cursorPosition() (int, int, bool) {
	output, err := tmuxCommand(b.tmuxSocketName, "display-message", "-p", "-t", b.tmuxSession, "#{cursor_x},#{cursor_y}").Output()
	if err != nil {
		return 0, 0, false
	}
	parts := strings.Split(strings.TrimSpace(string(output)), ",")
	if len(parts) != 2 {
		return 0, 0, false
	}
	x, errX := strconv.Atoi(parts[0])
	y, errY := strconv.Atoi(parts[1])
	if errX != nil || errY != nil || x < 0 || y < 0 {
		return 0, 0, false
	}
	return x, y, true
}

// snapshotPtmx returns the current ptmx *os.File under RLock. It returns
// nil if the bridge has been Closed. Consumers (WriteInput, streamOutput)
// use this to read the field race-free with respect to Close()'s
// Lock-guarded `b.ptmx = nil` store. The returned *os.File itself is
// goroutine-safe with respect to Close (Go's runtime poller handles
// Close vs. blocked I/O), so callers need not hold the RLock during the
// I/O syscall. (V1.9 T5, race-review 2.1.)
func (b *tmuxPTYBridge) snapshotPtmx() *os.File {
	b.ptmxMu.RLock()
	defer b.ptmxMu.RUnlock()
	return b.ptmx
}

func (b *tmuxPTYBridge) streamOutput() {
	defer close(b.done)

	buf := make([]byte, 4096)
	for {
		ptmx := b.snapshotPtmx()
		if ptmx == nil {
			return
		}
		n, err := ptmx.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if writeErr := b.writer.WriteBinary(chunk); writeErr != nil {
				b.Close()
				return
			}
		}

		if err != nil {
			if !errors.Is(err, io.EOF) {
				_ = b.writer.WriteJSON(wsServerMessage{
					Type:      "status",
					Event:     "session_closed",
					SessionID: b.sessionID,
					Time:      time.Now().UTC(),
				})
			}
			b.Close()
			return
		}
	}
}

func (b *tmuxPTYBridge) WriteInput(data string) error {
	if b == nil {
		return fmt.Errorf("bridge not initialized")
	}
	if data == "" {
		return nil
	}
	if b.polling {
		return b.sendPollingInput(data)
	}
	ptmx := b.snapshotPtmx()
	if ptmx == nil {
		return fmt.Errorf("bridge not initialized")
	}
	_, err := ptmx.Write([]byte(data))
	return err
}

func (b *tmuxPTYBridge) sendPollingInput(data string) error {
	for _, op := range splitPollingInput(data) {
		if op.literal != "" {
			if err := tmuxCommand(b.tmuxSocketName, "send-keys", "-l", "-t", b.tmuxSession, "--", op.literal).Run(); err != nil {
				return err
			}
			continue
		}
		if op.key != "" {
			if err := tmuxCommand(b.tmuxSocketName, "send-keys", "-t", b.tmuxSession, op.key).Run(); err != nil {
				return err
			}
		}
	}
	return b.captureAndWriteIfChanged(true)
}

type pollingInputOp struct {
	literal string
	key     string
}

func splitPollingInput(data string) []pollingInputOp {
	var ops []pollingInputOp
	var literal strings.Builder

	flushLiteral := func() {
		if literal.Len() == 0 {
			return
		}
		ops = append(ops, pollingInputOp{literal: literal.String()})
		literal.Reset()
	}

	for _, r := range data {
		switch r {
		case '\r', '\n':
			flushLiteral()
			ops = append(ops, pollingInputOp{key: "Enter"})
		case '\x03':
			flushLiteral()
			ops = append(ops, pollingInputOp{key: "C-c"})
		case '\x04':
			flushLiteral()
			ops = append(ops, pollingInputOp{key: "C-d"})
		case '\b', '\x7f':
			flushLiteral()
			ops = append(ops, pollingInputOp{key: "BSpace"})
		default:
			literal.WriteRune(r)
		}
	}
	flushLiteral()
	return ops
}

func formatPollingCaptureForTerminal(content string, cursorX, cursorY int, hasCursor bool) string {
	// capture-pane returns plain LF-delimited lines. xterm.js runs with
	// convertEol=false so LF moves down without carriage return, producing a
	// staircase layout. It also includes blank rows below the prompt; trim
	// those or the browser cursor lands at the bottom of the terminal instead
	// of the active input line.
	minRows := 0
	if hasCursor {
		minRows = cursorY + 1
	}
	trimmed := trimTrailingBlankCaptureRows(content, minRows)
	formatted := "\x1b[H\x1b[2J" + strings.ReplaceAll(trimmed, "\n", "\r\n")
	if hasCursor {
		formatted += fmt.Sprintf("\x1b[%d;%dH", cursorY+1, cursorX+1)
	}
	return formatted
}

func trimTrailingBlankCaptureRows(content string, minRows int) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	for len(lines) > minRows && isBlankCaptureRow(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

func isBlankCaptureRow(line string) bool {
	return strings.TrimSpace(stripSimpleANSI(line)) == ""
}

func stripSimpleANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) {
				c := s[i]
				i++
				if c >= 0x40 && c <= 0x7e {
					break
				}
			}
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func (b *tmuxPTYBridge) Resize(cols, rows int) error {
	if b == nil {
		return fmt.Errorf("bridge not initialized")
	}
	if cols <= 0 || rows <= 0 {
		return fmt.Errorf("invalid dimensions: cols=%d rows=%d", cols, rows)
	}
	if cols < 10 || rows < 3 {
		return fmt.Errorf("dimensions too small for a usable terminal: cols=%d rows=%d", cols, rows)
	}
	if b.polling {
		if err := b.resizeTmuxWindow(cols, rows); err != nil {
			return err
		}
		return b.captureAndWriteIfChanged(true)
	}

	b.ptmxMu.RLock()
	defer b.ptmxMu.RUnlock()
	if b.ptmx == nil {
		return fmt.Errorf("bridge not initialized")
	}

	// Resize the local PTY master. This sends SIGWINCH to the tmux attach
	// process. Because the attach client (see tmuxAttachCommand) is no longer
	// flagged `-f ignore-size`, the tmux server now uses this client's PTY
	// size as its declared geometry and re-arbitrates the window dimensions
	// per the session's `window-size` policy (`largest` — set at Session.Start
	// in internal/tmux/tmux.go). The previous `tmux resize-window` call here
	// was removed because it implicitly flipped the session option to
	// `window-size=manual` and pinned the window to the web viewport, which
	// dragged native attached clients (Ghostty, iTerm) along with it. Letting
	// tmux do the arbitration via `largest` keeps every client at the size of
	// the biggest viewer; smaller clients see a clipped portion of the larger
	// window content (no dot-filled void cells).
	if err := pty.Setsize(b.ptmx, &pty.Winsize{
		Rows: uint16(rows), // #nosec G115 -- terminal rows fits in uint16; PTY ABI enforces this
		Cols: uint16(cols), // #nosec G115 -- terminal cols fits in uint16; PTY ABI enforces this
	}); err != nil {
		return fmt.Errorf("resize pty: %w", err)
	}

	return nil
}

func (b *tmuxPTYBridge) resizeTmuxWindow(cols, rows int) error {
	args := []string{
		"resize-window", "-t", b.tmuxSession,
		"-x", strconv.Itoa(cols),
		"-y", strconv.Itoa(rows),
	}
	if output, err := tmuxCommand(b.tmuxSocketName, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("tmux resize-window: %w (output: %s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (b *tmuxPTYBridge) Close() {
	if b == nil {
		return
	}
	b.closeOnce.Do(func() {
		if b.pollStop != nil {
			close(b.pollStop)
		}
		b.ptmxMu.Lock()
		if b.ptmx != nil {
			_ = b.ptmx.Close()
			b.ptmx = nil
		}
		b.ptmxMu.Unlock()
		if b.cmd != nil && b.cmd.Process != nil {
			if err := processutil.TerminateProcessTree(b.cmd.Process); err != nil {
				_ = b.cmd.Process.Kill()
			}
		}
		if b.cmd != nil {
			_ = b.cmd.Wait()
		}
	})
}

func tmuxSessionExists(name, socketName string) (bool, error) {
	cmd := tmuxCommand(socketName, "has-session", "-t", name)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return true, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}

	msg := strings.TrimSpace(string(output))
	if msg == "" {
		msg = err.Error()
	}
	return false, fmt.Errorf("tmux has-session failed: %s", msg)
}

// tmuxCommand assembles an `exec.Cmd` for tmux, selecting the server in the
// following precedence order: (1) explicit socketName from the caller — the
// session's stored TmuxSocketName captured at creation time, passed through
// as tmux `-L <name>`; (2) TMUX env var's socket path (legacy web-in-tmux
// behavior), passed through as `-S <path>`; (3) tmux's default server. The
// legacy env-based fallback is preserved so running `agent-deck web` inside
// an existing tmux pane keeps working for users who haven't opted into the
// new per-session socket config (issue #687 phase 1).
func tmuxCommand(socketName string, args ...string) *exec.Cmd {
	// Explicit per-session socket name wins — this is the v1.7.50 path.
	if trimmed := strings.TrimSpace(socketName); trimmed != "" {
		finalArgs := append([]string{"-L", trimmed}, args...)
		cmd := exec.Command("tmux", finalArgs...)
		// Unset TMUX so tmux-in-tmux guards don't trip: we are explicitly
		// directing this to a different server than the one we're in.
		cmd.Env = environWithoutTMUX(os.Environ())
		return cmd
	}

	socketPath, hasSocket := tmuxSocketFromEnv()

	finalArgs := args
	if hasSocket {
		finalArgs = append([]string{"-S", socketPath}, args...)
	}

	cmd := exec.Command("tmux", finalArgs...)
	if hasSocket || runtime.GOOS == "windows" {
		cmd.Env = sanitizeTmuxEnv(os.Environ(), hasSocket)
	}
	return cmd
}

func tmuxAttachCommand(sessionName, socketName string) *exec.Cmd {
	// Web's attach is now a normal client whose PTY size participates in tmux's
	// `window-size=largest` arbitration (set at Session.Start). Previously we
	// passed `-f ignore-size` together with a manual `tmux resize-window` call
	// in (*tmuxPTYBridge).Resize; the manual resize-window flipped the session
	// option to `window-size=manual` and pinned the window to the web viewport
	// for ALL attached clients (Ghostty, iTerm) — the dots-in-window symptom.
	// With largest in effect, every client sees content sized to the biggest
	// viewer; smaller clients see a clipped portion rather than dot-filled void.
	return tmuxCommand(socketName, "attach-session", "-t", sessionName)
}

func tmuxSocketFromEnv() (string, bool) {
	raw := strings.TrimSpace(os.Getenv("TMUX"))
	if raw == "" {
		return "", false
	}

	socketPart := raw
	if strings.Contains(raw, ",") {
		socketPart = strings.SplitN(raw, ",", 2)[0]
	}

	socketPart = strings.TrimSpace(socketPart)
	if socketPart == "" {
		return "", false
	}
	return socketPart, true
}

func sanitizeTmuxEnv(env []string, stripTMUX bool) []string {
	filtered := make([]string, 0, len(env))
	for _, kv := range env {
		if stripTMUX && strings.HasPrefix(kv, "TMUX=") {
			continue
		}
		if runtime.GOOS == "windows" && strings.HasPrefix(kv, "PSMUX_SESSION=") {
			continue
		}
		filtered = append(filtered, kv)
	}
	return filtered
}

func environWithoutTMUX(env []string) []string {
	return sanitizeTmuxEnv(env, true)
}

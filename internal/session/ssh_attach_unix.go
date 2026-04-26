//go:build !windows

package session

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/termreply"
	"github.com/asheshgoplani/agent-deck/internal/tmux"
	"github.com/creack/pty"
	"golang.org/x/term"
)

const sshAttachReplyQuarantine = 2 * time.Second

func (r *SSHRunner) attachWithPTY(remoteCmd string) error {
	sshArgs := []string{
		"-tt",
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=" + sshControlDir + "/%r@%h:%p",
		"-o", "ControlPersist=600",
		r.Host,
		remoteCmd,
	}

	cmd := exec.Command("ssh", sshArgs...)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("failed to start ssh with pty: %w", err)
	}
	defer ptmx.Close()

	if _, err := term.MakeRaw(int(ptmx.Fd())); err != nil {
		return fmt.Errorf("failed to set pty raw mode: %w", err)
	}

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("failed to set raw mode: %w", err)
	}
	defer func() { _ = term.Restore(int(os.Stdin.Fd()), oldState) }()

	sigwinch := make(chan os.Signal, 1)
	signal.Notify(sigwinch, syscall.SIGWINCH)
	sigwinchDone := make(chan struct{})
	defer func() {
		signal.Stop(sigwinch)
		close(sigwinchDone)
	}()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-sigwinchDone:
				return
			case _, ok := <-sigwinch:
				if !ok {
					return
				}
				if ws, err := pty.GetsizeFull(os.Stdin); err == nil {
					_ = pty.Setsize(ptmx, ws)
				}
			}
		}
	}()
	sigwinch <- syscall.SIGWINCH

	detachCh := make(chan struct{})
	outputDone := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(outputDone)
		_, _ = io.Copy(os.Stdout, ptmx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 256)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				break
			}
			data := buf[:n]
			if idx := tmux.IndexCtrlQ(data); idx >= 0 {
				if idx > 0 {
					_, _ = ptmx.Write(data[:idx])
				}
				close(detachCh)
				return
			}
			if _, err := ptmx.Write(data); err != nil {
				break
			}
		}
	}()

	cmdDone := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		cmdDone <- cmd.Wait()
	}()

	select {
	case <-detachCh:
	case <-cmdDone:
	}

	_ = ptmx.Close()
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	select {
	case <-outputDone:
	case <-time.After(50 * time.Millisecond):
	}
	termreply.QuarantineFor(sshAttachReplyQuarantine)
	_, _ = os.Stdout.WriteString("\x1b]8;;\x1b\\\x1b[0m\x1b[24m\x1b[39m\x1b[49m")
	if p, err := os.FindProcess(os.Getpid()); err == nil {
		_ = p.Signal(syscall.SIGWINCH)
	}
	return nil
}

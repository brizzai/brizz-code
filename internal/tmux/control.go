package tmux

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/brizzai/fleet/internal/debuglog"
	"github.com/creack/pty"
)

// ControlClient maintains a persistent tmux control mode connection
// for low-latency command execution (especially send-keys).
// Instead of spawning a new process per command (~5-15ms), commands
// are written as text to a PTY file descriptor (~0.1ms).
type ControlClient struct {
	mu          sync.Mutex
	ptmx        *os.File
	cmd         *exec.Cmd
	sessionName string
	closed      bool
}

// NewControlClient starts a persistent tmux control mode connection.
// It creates a hidden tmux session (without the fleet_ prefix) and
// attaches a control mode client to it via a PTY.
func NewControlClient() (*ControlClient, error) {
	name := "_fleet_ctrl_" + generateShortID()

	cmd := exec.Command("tmux", "-C", "new-session", "-s", name)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("tmux control mode start failed: %w", err)
	}

	cc := &ControlClient{
		ptmx:        ptmx,
		cmd:         cmd,
		sessionName: name,
	}

	// Drain output to prevent buffer blocking.
	go io.Copy(io.Discard, ptmx)

	// Suppress output notifications for performance.
	cc.writeCommand("refresh-client -f no-output")

	debuglog.Logger.Info("tmux control client started", "session", name)
	return cc, nil
}

func (cc *ControlClient) writeCommand(cmd string) error {
	_, err := fmt.Fprintf(cc.ptmx, "%s\n", cmd)
	return err
}

// SendKeys sends named keys to a target tmux session.
func (cc *ControlClient) SendKeys(targetSession string, keys ...string) error {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if cc.closed {
		return fmt.Errorf("control client closed")
	}
	args := strings.Join(keys, " ")
	return cc.writeCommand(fmt.Sprintf("send-keys -t %s %s", targetSession, args))
}

// SendLiteralKeys sends literal text to a target tmux session.
func (cc *ControlClient) SendLiteralKeys(targetSession, text string) error {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if cc.closed {
		return fmt.Errorf("control client closed")
	}
	return cc.writeCommand(fmt.Sprintf("send-keys -t %s -l %s", targetSession, quoteTmux(text)))
}

// IsClosed returns whether the control client has been closed.
func (cc *ControlClient) IsClosed() bool {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	return cc.closed
}

// Close terminates the control client and its hidden session.
func (cc *ControlClient) Close() {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if cc.closed {
		return
	}
	cc.closed = true
	// Kill the control session.
	_ = cc.writeCommand("kill-session -t " + cc.sessionName)
	cc.ptmx.Close()
	_ = cc.cmd.Wait()
	debuglog.Logger.Info("tmux control client closed", "session", cc.sessionName)
}

// quoteTmux wraps text in single quotes for tmux command parsing.
// Embedded single quotes are escaped with the standard shell pattern.
func quoteTmux(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

//go:build !windows

package tmux

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

const terminalStyleReset = "\x1b]8;;\x1b\\\x1b[0m\x1b[24m\x1b[39m\x1b[49m"

// Attach attaches to the tmux session with full PTY support.
// Ctrl+Q detaches and returns to the caller. Ctrl+b d also works (tmux native).
func (s *Session) Attach(ctx context.Context) error {
	if !s.Exists() {
		return fmt.Errorf("session %s does not exist", s.Name)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Start tmux attach with PTY.
	cmd := exec.CommandContext(ctx, "tmux", "attach-session", "-t", s.Name)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("failed to start pty: %w", err)
	}
	defer ptmx.Close()

	// Save terminal state and set raw mode.
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("failed to set raw mode: %w", err)
	}
	defer func() { _ = term.Restore(int(os.Stdin.Fd()), oldState) }()

	// Handle window resize signals.
	sigwinch := make(chan os.Signal, 1)
	signal.Notify(sigwinch, syscall.SIGWINCH)
	sigwinchDone := make(chan struct{})
	defer func() {
		signal.Stop(sigwinch)
		close(sigwinchDone)
	}()

	var wg sync.WaitGroup

	// SIGWINCH handler.
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
	// Initial resize.
	sigwinch <- syscall.SIGWINCH

	detachCh := make(chan struct{})
	ioErrors := make(chan error, 2)
	startTime := time.Now()
	const controlSeqTimeout = 50 * time.Millisecond
	outputDone := make(chan struct{})

	// Goroutine: copy PTY output to stdout.
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(outputDone)
		_, err := io.Copy(os.Stdout, ptmx)
		if err != nil && err != io.EOF {
			select {
			case ioErrors <- fmt.Errorf("PTY read error: %w", err):
			default:
			}
		}
	}()

	// Goroutine: read stdin, intercept Ctrl+Q (ASCII 17), forward rest to PTY.
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 32)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				if err != io.EOF {
					select {
					case ioErrors <- fmt.Errorf("stdin read error: %w", err):
					default:
					}
				}
				return
			}

			// Discard initial terminal control sequences.
			if time.Since(startTime) < controlSeqTimeout {
				continue
			}

			// Check for Ctrl+Q (ASCII 17).
			if idx := bytes.IndexByte(buf[:n], 17); idx >= 0 {
				if idx > 0 {
					if _, werr := ptmx.Write(buf[:idx]); werr != nil {
						select {
						case ioErrors <- fmt.Errorf("PTY write error: %w", werr):
						default:
						}
						return
					}
				}
				close(detachCh)
				cancel()
				return
			}

			// Forward input to PTY.
			if _, werr := ptmx.Write(buf[:n]); werr != nil {
				select {
				case ioErrors <- fmt.Errorf("PTY write error: %w", werr):
				default:
				}
				return
			}
		}
	}()

	// Wait for command to finish.
	cmdDone := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		cmdDone <- cmd.Wait()
	}()

	cleanupAttach := func() {
		cancel()
		_ = ptmx.Close()
		select {
		case <-outputDone:
		case <-time.After(20 * time.Millisecond):
		}
		_, _ = os.Stdout.WriteString(terminalStyleReset)
	}

	// Wait for detach or command completion.
	var attachErr error
	select {
	case <-detachCh:
		attachErr = nil
	case err := <-cmdDone:
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				if exitErr.ExitCode() == 0 || exitErr.ExitCode() == 1 {
					attachErr = nil
				} else {
					attachErr = err
				}
			} else {
				attachErr = err
			}
			if ctx.Err() != nil {
				attachErr = nil
			}
		}
	case <-ctx.Done():
		attachErr = nil
	}

	cleanupAttach()
	return attachErr
}

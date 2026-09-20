package terminal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/GrayCodeAI/rho/internal/env"
)

var (
	// ErrUnauthorizedSession is returned when a session attempts to operate on a terminal owned by another session.
	ErrUnauthorizedSession = errors.New("terminal: unauthorized access (terminal belongs to another session)")
	// ErrTerminalNotFound is returned when a requested terminal ID does not exist.
	ErrTerminalNotFound = errors.New("terminal: terminal not found")
	// ErrTerminalClosed is returned when an operation is performed on a terminated terminal.
	ErrTerminalClosed = errors.New("terminal: terminal is closed")
)

// DefaultReadCap is the maximum number of bytes returned by a single read call (64 KiB).
const DefaultReadCap = 64 * 1024

// TerminalInfo provides metadata about an active or terminated terminal.
type TerminalInfo struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Command   string    `json:"command"`
	CWD       string    `json:"cwd"`
	CreatedAt time.Time `json:"created_at"`
	Alive     bool      `json:"alive"`
	ExitCode  int       `json:"exit_code,omitempty"`
}

// Terminal represents an active persistent PTY session.
type Terminal struct {
	ID        string
	SessionID string
	Command   string
	CWD       string
	CreatedAt time.Time

	cmd    *exec.Cmd
	device *ptyDevice

	mu       sync.Mutex
	cond     *sync.Cond
	buf      bytes.Buffer
	closed   bool
	alive    bool
	readDone bool
	exitCode int
}

// Send writes user input to the terminal PTY.
func (t *Terminal) Send(input string, enter bool) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed || !t.alive {
		return ErrTerminalClosed
	}

	data := []byte(input)
	if enter && !bytes.HasSuffix(data, []byte("\n")) {
		data = append(data, '\n')
	}

	_, err := t.device.Write(data)
	return err
}

// Read reads pending output bytes from the terminal with a bounded wait timeout.
func (t *Terminal) Read(maxBytes int, timeout time.Duration) (string, bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if maxBytes <= 0 || maxBytes > DefaultReadCap {
		maxBytes = DefaultReadCap
	}

	// If no data and timeout specified, wait on cond
	if t.buf.Len() == 0 && timeout > 0 && !t.closed && !t.readDone {
		timer := time.AfterFunc(timeout, func() {
			t.mu.Lock()
			t.cond.Broadcast()
			t.mu.Unlock()
		})
		defer timer.Stop()

		for t.buf.Len() == 0 && !t.closed && !t.readDone {
			t.cond.Wait()
			break
		}
	}

	if t.buf.Len() == 0 {
		return "", t.alive, nil
	}

	toRead := maxBytes
	if t.buf.Len() < toRead {
		toRead = t.buf.Len()
	}

	out := make([]byte, toRead)
	_, _ = t.buf.Read(out)

	return string(out), t.alive, nil
}

// Resize resizes the terminal PTY window.
func (t *Terminal) Resize(rows, cols int) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed || !t.alive {
		return ErrTerminalClosed
	}
	return t.device.Resize(rows, cols)
}

// Kill terminates the terminal process and releases associated resources.
func (t *Terminal) Kill() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.alive = false
	t.mu.Unlock()

	if t.device != nil {
		_ = t.device.Close()
	}
	if t.cmd != nil && t.cmd.Process != nil {
		_ = killProcessTree(t.cmd.Process)
	}

	t.mu.Lock()
	t.cond.Broadcast()
	t.mu.Unlock()

	return nil
}

// Info returns a snapshot of the terminal's status.
func (t *Terminal) Info() *TerminalInfo {
	t.mu.Lock()
	defer t.mu.Unlock()

	return &TerminalInfo{
		ID:        t.ID,
		SessionID: t.SessionID,
		Command:   t.Command,
		CWD:       t.CWD,
		CreatedAt: t.CreatedAt,
		Alive:     t.alive,
		ExitCode:  t.exitCode,
	}
}

// Store coordinates and persists active PTY terminals.
type Store struct {
	mu        sync.RWMutex
	terminals map[string]*Terminal
	nextSeq   int
}

// NewStore creates a new TerminalStore.
func NewStore() *Store {
	return &Store{
		terminals: make(map[string]*Terminal),
	}
}

// Global default store instance
var (
	defaultStore     *Store
	defaultStoreOnce sync.Once
)

// DefaultStore returns the global shared TerminalStore.
func DefaultStore() *Store {
	defaultStoreOnce.Do(func() {
		defaultStore = NewStore()
	})
	return defaultStore
}

// Create spawns a new persistent PTY terminal under session ownership.
func (s *Store) Create(ctx context.Context, sessionID, cwd, command string, rows, cols int) (*Terminal, error) {
	if sessionID == "" {
		return nil, errors.New("terminal: sessionID cannot be empty")
	}

	if cwd == "" {
		if wd, err := os.Getwd(); err == nil {
			cwd = wd
		}
	}
	cwd = filepath.Clean(cwd)

	if command == "" {
		if shell := os.Getenv("SHELL"); shell != "" {
			command = shell
		} else if runtime.GOOS == "windows" {
			command = "powershell.exe"
		} else {
			command = "/bin/bash"
		}
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "powershell.exe", "-Command", command) // #nosec G204 -- subprocess execution of shell command is the primary responsibility of terminal package
	} else {
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", command) // #nosec G204 -- subprocess execution of shell command is the primary responsibility of terminal package
	}
	cmd.Dir = cwd
	// Never hand provider API keys to a terminal child process. The agent can
	// drive this shell, so it must not be able to read ANTHROPIC_API_KEY etc.
	// from the inherited environment.
	cmd.Env = env.SubprocessEnv()

	device, err := startPTY(cmd, rows, cols)
	if err != nil {
		return nil, fmt.Errorf("terminal: pty spawn failed: %w", err)
	}

	s.mu.Lock()
	s.nextSeq++
	termID := fmt.Sprintf("terminal-%d", s.nextSeq)

	t := &Terminal{
		ID:        termID,
		SessionID: sessionID,
		Command:   command,
		CWD:       cwd,
		CreatedAt: time.Now(),
		cmd:       cmd,
		device:    device,
		alive:     true,
	}
	t.cond = sync.NewCond(&t.mu)
	s.terminals[termID] = t
	s.mu.Unlock()

	// 1. Background output reader
	go func() {
		buf := make([]byte, 4096)
		for {
			n, rErr := device.Read(buf)
			if n > 0 {
				t.mu.Lock()
				// Cap internal buffer at 512 KiB to prevent unbounded memory growth
				if t.buf.Len()+n > 512*1024 {
					drop := (t.buf.Len() + n) - 512*1024
					_ = t.buf.Next(drop)
				}
				t.buf.Write(buf[:n])
				t.cond.Broadcast()
				t.mu.Unlock()
			}
			if rErr != nil {
				t.mu.Lock()
				t.readDone = true
				t.cond.Broadcast()
				t.mu.Unlock()
				break
			}
		}
	}()

	// 2. Background process waiter
	go func() {
		wErr := cmd.Wait()
		t.mu.Lock()
		t.alive = false
		if wErr != nil {
			var exitErr *exec.ExitError
			if errors.As(wErr, &exitErr) {
				t.exitCode = exitErr.ExitCode()
			} else {
				t.exitCode = 1
			}
		} else {
			t.exitCode = 0
		}
		t.cond.Broadcast()
		t.mu.Unlock()
	}()

	return t, nil
}

// Get fetches a terminal verifying exact session ownership.
func (s *Store) Get(sessionID, id string) (*Terminal, error) {
	s.mu.RLock()
	t, ok := s.terminals[id]
	s.mu.RUnlock()

	if !ok {
		return nil, ErrTerminalNotFound
	}
	if sessionID != "" && t.SessionID != sessionID {
		return nil, fmt.Errorf("%w (session %s cannot access terminal owned by %s)",
			ErrUnauthorizedSession, sessionID, t.SessionID)
	}
	return t, nil
}

// List returns info for all terminals owned by the calling session.
func (s *Store) List(sessionID string) []*TerminalInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var list []*TerminalInfo
	for _, t := range s.terminals {
		if sessionID == "" || t.SessionID == sessionID {
			list = append(list, t.Info())
		}
	}
	return list
}

// Delete removes and terminates a specific terminal.
func (s *Store) Delete(sessionID, id string) error {
	t, err := s.Get(sessionID, id)
	if err != nil {
		return err
	}

	_ = t.Kill()

	s.mu.Lock()
	delete(s.terminals, id)
	s.mu.Unlock()

	return nil
}

// CloseSession gracefully disposes all terminals owned by sessionID.
func (s *Store) CloseSession(sessionID string) {
	if sessionID == "" {
		return
	}
	s.mu.Lock()
	var toClose []*Terminal
	for id, t := range s.terminals {
		if t.SessionID == sessionID {
			toClose = append(toClose, t)
			delete(s.terminals, id)
		}
	}
	s.mu.Unlock()

	for _, t := range toClose {
		_ = t.Kill()
	}
}

// killProcessTree terminates the process and any descendants.
func killProcessTree(p *os.Process) error {
	if p == nil {
		return nil
	}
	return killProcessGroup(p)
}

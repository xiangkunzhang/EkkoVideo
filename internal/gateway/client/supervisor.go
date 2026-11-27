// Package client supervises the VelanGateway child process and provides an
// authenticated HTTP client for the main GUI process.
package client

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/xiangkunzhang/Velan/internal/gateway/server"
	"github.com/xiangkunzhang/Velan/internal/infra/safego"
)

// Status describes the current state of the supervised gateway child process.
type Status struct {
	Running   bool
	Addr      string
	PID       int
	Restarts  int
	LastError string
}

// Supervisor launches, monitors, and auto-restarts the --gateway subprocess.
type Supervisor struct {
	executable string
	args       []string
	logf       func(string, ...any)
	onChange   func(Status)
	httpClient *http.Client

	mu        sync.Mutex
	stopped   bool
	token     string
	cmd       *exec.Cmd
	waitDone  chan struct{}
	stdinPipe io.WriteCloser
	status    Status
	restarts  []time.Time
}

// NewSupervisor creates a supervisor for the gateway subprocess.
// If executable is empty, os.Executable() with ["--gateway", "--port", "0"] is used.
func NewSupervisor(executable string, args []string, logf func(string, ...any), onChange func(Status)) *Supervisor {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Supervisor{
		executable: executable,
		args:       args,
		logf:       logf,
		onChange:   onChange,
		httpClient: &http.Client{Timeout: 25 * time.Second},
	}
}

// Start launches the gateway subprocess and returns once the first handshake succeeds.
func (s *Supervisor) Start(ctx context.Context) error {
	if err := s.spawnOnce(ctx); err != nil {
		return err
	}
	return nil
}

// Status returns a snapshot of the supervisor state.
func (s *Supervisor) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// Health queries the gateway /health endpoint.
func (s *Supervisor) Health(ctx context.Context) error {
	s.mu.Lock()
	addr := s.status.Addr
	token := s.token
	s.mu.Unlock()
	if addr == "" {
		return errors.New("gateway not running")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/health", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gateway health status %d", resp.StatusCode)
	}
	return nil
}

// KillChild terminates the active gateway child process (used for testing auto-recovery).
func (s *Supervisor) KillChild() {
	s.mu.Lock()
	cmd := s.cmd
	s.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

// Stop shuts down the supervisor and closes the child's stdin pipe so it exits cleanly.
func (s *Supervisor) Stop() {
	s.mu.Lock()
	s.stopped = true
	pipe := s.stdinPipe
	cmd := s.cmd
	done := s.waitDone
	s.stdinPipe = nil
	s.cmd = nil
	s.waitDone = nil
	s.status.Running = false
	s.mu.Unlock()

	if pipe != nil {
		_ = pipe.Close()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			if cmd != nil && cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			<-done
		}
	}
}

func (s *Supervisor) spawnOnce(ctx context.Context) error {
	exe := s.executable
	args := s.args
	if exe == "" {
		var err error
		exe, err = os.Executable()
		if err != nil {
			return err
		}
		args = []string{"--gateway", "--port", "0"}
	}

	token := randomToken()
	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), "VELAN_GW_TOKEN="+token)
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdinPipe.Close()
		return err
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		_ = stdinPipe.Close()
		return fmt.Errorf("start gateway: %w", err)
	}

	hsCh := make(chan server.Handshake, 1)
	errCh := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(stdoutPipe)
		if !sc.Scan() {
			if err := sc.Err(); err != nil {
				errCh <- err
			} else {
				errCh <- errors.New("gateway exited before handshake")
			}
			return
		}
		var hs server.Handshake
		if err := json.Unmarshal(sc.Bytes(), &hs); err != nil {
			errCh <- fmt.Errorf("invalid gateway handshake: %w", err)
			return
		}
		hsCh <- hs
		// Drain remaining stdout so child never blocks on write.
		_, _ = io.Copy(io.Discard, stdoutPipe)
	}()

	var hs server.Handshake
	select {
	case hs = <-hsCh:
	case err := <-errCh:
		_ = stdinPipe.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	case <-time.After(5 * time.Second):
		_ = stdinPipe.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return errors.New("gateway handshake timeout")
	case <-ctx.Done():
		_ = stdinPipe.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return ctx.Err()
	}

	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		_ = stdinPipe.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return errors.New("supervisor stopped")
	}
	waitDone := make(chan struct{})
	s.token = token
	s.cmd = cmd
	s.waitDone = waitDone
	s.stdinPipe = stdinPipe
	s.status.Running = true
	s.status.Addr = hs.Addr
	s.status.PID = hs.PID
	s.status.LastError = ""
	st := s.status
	s.mu.Unlock()

	s.logf("gateway: started pid=%d addr=%s", hs.PID, hs.Addr)
	s.notify(st)

	safego.Go("gateway-watch", s.logf, func() {
		waitErr := cmd.Wait()
		close(waitDone)
		s.watchExit(cmd, waitErr)
	})
	return nil
}

func (s *Supervisor) watchExit(cmd *exec.Cmd, waitErr error) {
	s.mu.Lock()
	if s.stopped || s.cmd != cmd {
		s.mu.Unlock()
		return
	}
	s.status.Running = false
	s.status.Addr = ""
	s.status.PID = 0
	if waitErr != nil {
		s.status.LastError = waitErr.Error()
	} else {
		s.status.LastError = "exited unexpectedly"
	}

	now := time.Now()
	recent := s.restarts[:0]
	for _, t := range s.restarts {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	s.restarts = recent
	if len(s.restarts) >= 3 {
		s.status.LastError = "gateway restart limit reached (3/min)"
		st := s.status
		s.mu.Unlock()
		s.logf("gateway: %s", st.LastError)
		s.notify(st)
		return
	}
	attempt := len(s.restarts)
	s.restarts = append(s.restarts, now)
	s.status.Restarts++
	st := s.status
	s.mu.Unlock()

	s.logf("gateway: crashed (%v), restarting...", waitErr)
	s.notify(st)

	backoff := []time.Duration{200 * time.Millisecond, 500 * time.Millisecond, 1 * time.Second}[min(attempt, 2)]
	time.Sleep(backoff)

	s.mu.Lock()
	stopped := s.stopped
	s.mu.Unlock()
	if stopped {
		return
	}
	if err := s.spawnOnce(context.Background()); err != nil {
		s.mu.Lock()
		s.status.LastError = err.Error()
		st := s.status
		s.mu.Unlock()
		s.logf("gateway: restart failed: %v", err)
		s.notify(st)
	}
}

func (s *Supervisor) notify(st Status) {
	if s.onChange != nil {
		s.onChange(st)
	}
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

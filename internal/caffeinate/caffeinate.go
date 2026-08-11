package caffeinate

import (
	"fmt"
	"os/exec"
	"sync"
	"time"
)

type Status struct {
	Active    bool   `json:"active"`
	Mode      string `json:"mode"`
	StartedAt string `json:"started_at,omitempty"`
	PID       int    `json:"pid,omitempty"`
	Reason    string `json:"reason"`
	Uptime    string `json:"uptime,omitempty"`
	Manual    bool   `json:"manual"`
}

type Manager struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	startedAt time.Time
	manual    bool
	reason    string
}

func NewManager() *Manager {
	return &Manager{}
}

func (m *Manager) Start(reason string, manual bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd != nil {
		m.reason = reason
		m.manual = m.manual || manual
		return
	}
	m.spawn(reason, manual)
}

func (m *Manager) Stop(force bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd == nil {
		return
	}
	if m.manual && !force {
		return
	}
	m.kill()
}

func (m *Manager) SetAutomatic(running int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if running > 0 && m.cmd == nil {
		m.spawn("tasks running", false)
		return
	}
	if running == 0 && m.cmd != nil && !m.manual {
		m.kill()
	}
}

func (m *Manager) Toggle() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd != nil {
		m.kill()
		return false
	}
	m.spawn("manual", true)
	return true
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd == nil {
		return Status{Active: false, Mode: "off", Reason: "idle"}
	}
	s := Status{
		Active:    true,
		Mode:      "display+idle",
		StartedAt: m.startedAt.Format(time.RFC3339),
		Reason:    m.reason,
		Manual:    m.manual,
	}
	if m.cmd.Process != nil {
		s.PID = m.cmd.Process.Pid
	}
	s.Uptime = formatUptime(time.Since(m.startedAt))
	return s
}

func (m *Manager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd != nil {
		m.kill()
	}
}

func (m *Manager) spawn(reason string, manual bool) {
	cmd := exec.Command("/usr/bin/caffeinate", "-di")
	if err := cmd.Start(); err != nil {
		return
	}
	m.cmd = cmd
	m.startedAt = time.Now()
	m.reason = reason
	m.manual = manual

	go func() {
		cmd.Wait()
		m.mu.Lock()
		if m.cmd == cmd {
			m.cmd = nil
		}
		m.mu.Unlock()
	}()
}

func (m *Manager) kill() {
	if m.cmd != nil && m.cmd.Process != nil {
		m.cmd.Process.Kill()
		m.cmd.Wait()
	}
	m.cmd = nil
	m.manual = false
	m.reason = ""
}

func formatUptime(d time.Duration) string {
	h := int(d.Hours())
	min := int(d.Minutes()) % 60
	sec := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, min)
	}
	if min > 0 {
		return fmt.Sprintf("%dm %ds", min, sec)
	}
	return fmt.Sprintf("%ds", sec)
}

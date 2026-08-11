// Package ble connects to the "burnboi" ESP32 display over Bluetooth Low Energy.
// It pushes task and usage data every few seconds and listens for voice-task
// creation button presses (START/STOP on the MIC characteristic).
package ble

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"tinygo.org/x/bluetooth"

	"github.com/Verhum/burnrate/internal/domain"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	deviceName   = "burnboi"
	pollInterval = 3 * time.Second
	scanTimeout  = 30 * time.Second
	maxBackoff   = 60 * time.Second
	payloadMax   = 500 // BLE characteristic max bytes
	maxRecordSec = 120 // auto-stop recording after 2 minutes

	defaultLat     = 33.749
	defaultLon     = -84.388
	defaultLocName = "Atlanta, GA"

	serialResetAfterScans = 3 // attempt USB serial reset after this many failed scans
)

// BLE service and characteristic UUIDs for the burnboi ESP32.
var (
	serviceUUID    = mustParseUUID("cb0a0001-5b20-4c5a-9b3d-8a7e6f1d2c3b")
	burnrateCharID = mustParseUUID("cb0a0002-5b20-4c5a-9b3d-8a7e6f1d2c3b") // write
	micCharID      = mustParseUUID("cb0a0003-5b20-4c5a-9b3d-8a7e6f1d2c3b") // notify

	// Tasks in these statuses are excluded from the BLE display.
	excludedStatuses = map[string]bool{
		"done":      true,
		"dismissed": true,
		"failed":    true,
	}
)

// ---------------------------------------------------------------------------
// Interfaces
// ---------------------------------------------------------------------------

// StoreReader provides read access to tasks and usage snapshots.
type StoreReader interface {
	ListTasks() ([]domain.Task, error)
	LatestUsageSnapshot() (*domain.UsageSnapshot, error)
}

// Logger matches the burnrate log.Logger interface.
type Logger interface {
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
	Debugf(format string, args ...any)
}

// ---------------------------------------------------------------------------
// BLE payload types
// ---------------------------------------------------------------------------

type blePayload struct {
	Pct     int         `json:"pct"`
	Spend   int         `json:"spend"`
	Limit   int         `json:"limit"`
	Pending int         `json:"pending"`
	Running int         `json:"running"`
	Resets  string      `json:"resets,omitempty"`
	Req     int         `json:"req"`
	Tickets []bleTicket `json:"tickets"`
	Lat     float64     `json:"lat,omitempty"`
	Lon     float64     `json:"lon,omitempty"`
	LocName string      `json:"loc_name,omitempty"`
	Hist    []float64   `json:"hist,omitempty"`
}

type bleTicket struct {
	ID     string `json:"id"`
	Status int    `json:"s"`
	Name   string `json:"n,omitempty"`
}

// ---------------------------------------------------------------------------
// Bridge
// ---------------------------------------------------------------------------

// Bridge connects to the burnboi ESP32 display over BLE.
type Bridge struct {
	store  StoreReader
	port   int // local HTTP API port for voice task creation
	logger Logger

	mu      sync.Mutex
	lat     float64
	lon     float64
	locName string
	locSent bool // once true, loc_name is omitted from payloads

	recMu   sync.Mutex
	recCmd  *exec.Cmd
	recFile string
}

// New creates a BLE bridge. port is the local burnrate HTTP server port, used
// to POST voice task creation requests.
func New(store StoreReader, port int, logger Logger) *Bridge {
	return &Bridge{
		store:   store,
		port:    port,
		logger:  logger,
		lat:     defaultLat,
		lon:     defaultLon,
		locName: defaultLocName,
	}
}

// Start runs the BLE bridge until ctx is canceled. It scans for the burnboi
// device, connects, and enters a poll-and-push loop. On disconnect it retries
// with exponential backoff capped at maxBackoff. If the bluetooth adapter
// fails to initialize, it retries indefinitely rather than dying.
func (b *Bridge) Start(ctx context.Context) {
	b.killStaleRecordings()
	b.resolveLocation()

	adapterBackoff := 5 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		adapter := bluetooth.DefaultAdapter
		if err := adapter.Enable(); err != nil {
			b.logger.Errorf("ble: failed to enable bluetooth adapter: %v; retrying in %v", err, adapterBackoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(adapterBackoff):
			}
			adapterBackoff = min(adapterBackoff*2, maxBackoff)
			continue
		}
		b.logger.Infof("ble: bluetooth adapter enabled")

		b.runLoop(ctx, adapter)
		if ctx.Err() != nil {
			return
		}
		// runLoop returned unexpectedly (adapter crashed) — restart everything
		b.logger.Warnf("ble: adapter loop exited; restarting in %v", adapterBackoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(adapterBackoff):
		}
	}
}

// runLoop scans, connects, and pushes in a loop, retrying on disconnect.
func (b *Bridge) runLoop(ctx context.Context, adapter *bluetooth.Adapter) {
	backoff := time.Second
	failedScans := 0
	for {
		if ctx.Err() != nil {
			return
		}

		wasConnected, err := b.run(ctx, adapter)
		if ctx.Err() != nil {
			return
		}
		if wasConnected {
			backoff = time.Second
			failedScans = 0
		} else {
			failedScans++
			if failedScans >= serialResetAfterScans {
				b.resetViaSerial()
				failedScans = 0
			}
		}
		if err != nil {
			b.logger.Warnf("ble: %v; reconnecting in %v", err, backoff)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

// run performs one scan-connect-serve cycle. wasConnected is true when a BLE
// connection was established, even if it later failed during polling.
func (b *Bridge) run(ctx context.Context, adapter *bluetooth.Adapter) (wasConnected bool, err error) {
	addr, err := b.scan(ctx, adapter)
	if err != nil {
		return false, err
	}

	b.logger.Infof("ble: connecting to %s...", deviceName)
	device, err := adapter.Connect(addr, bluetooth.ConnectionParams{})
	if err != nil {
		return false, fmt.Errorf("connect: %w", err)
	}
	defer device.Disconnect()
	wasConnected = true

	// Discover the burnboi service.
	services, err := device.DiscoverServices([]bluetooth.UUID{serviceUUID})
	if err != nil {
		return true, fmt.Errorf("discover services: %w", err)
	}
	if len(services) == 0 {
		return true, fmt.Errorf("burnboi service not found")
	}

	// Discover the two characteristics we need.
	chars, err := services[0].DiscoverCharacteristics([]bluetooth.UUID{burnrateCharID, micCharID})
	if err != nil {
		return true, fmt.Errorf("discover characteristics: %w", err)
	}

	var burnrateChar, micChar *bluetooth.DeviceCharacteristic
	for i := range chars {
		switch {
		case chars[i].UUID() == burnrateCharID:
			burnrateChar = &chars[i]
		case chars[i].UUID() == micCharID:
			micChar = &chars[i]
		}
	}
	if burnrateChar == nil || micChar == nil {
		return true, fmt.Errorf("required characteristics not found (burnrate=%v mic=%v)",
			burnrateChar != nil, micChar != nil)
	}

	// Subscribe to START/STOP notifications from the mic button.
	if err := micChar.EnableNotifications(func(buf []byte) {
		b.handleMicNotification(ctx, strings.TrimSpace(string(buf)))
	}); err != nil {
		return true, fmt.Errorf("enable mic notifications: %w", err)
	}

	b.logger.Infof("ble: connected to %s", deviceName)

	// Immediate first push, then every pollInterval.
	if err := b.pushPayload(burnrateChar); err != nil {
		return true, fmt.Errorf("initial push: %w", err)
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return true, nil
		case <-ticker.C:
			if err := b.pushPayload(burnrateChar); err != nil {
				return true, fmt.Errorf("push: %w", err)
			}
		}
	}
}

// scan blocks until the burnboi device is discovered or ctx expires.
func (b *Bridge) scan(ctx context.Context, adapter *bluetooth.Adapter) (addr bluetooth.Address, err error) {
	b.logger.Infof("ble: scanning for %s...", deviceName)

	addrCh := make(chan bluetooth.Address, 1)

	scanCtx, cancel := context.WithTimeout(ctx, scanTimeout)
	defer cancel()

	// Stop scanning when the timeout fires or the parent context is canceled.
	go func() {
		<-scanCtx.Done()
		adapter.StopScan()
	}()

	// Scan blocks until StopScan is called.
	_ = adapter.Scan(func(a *bluetooth.Adapter, result bluetooth.ScanResult) {
		if result.LocalName() == deviceName {
			select {
			case addrCh <- result.Address:
			default:
			}
			a.StopScan()
		}
	})

	select {
	case addr = <-addrCh:
		b.logger.Infof("ble: found %s", deviceName)
		return addr, nil
	default:
		if ctx.Err() != nil {
			return addr, ctx.Err()
		}
		return addr, fmt.Errorf("scan: %s not found", deviceName)
	}
}

// ---------------------------------------------------------------------------
// Payload building and pushing
// ---------------------------------------------------------------------------

// pushPayload reads tasks and usage from the store, builds the JSON payload,
// and writes it to the BLE burnrate characteristic.
func (b *Bridge) pushPayload(char *bluetooth.DeviceCharacteristic) error {
	tasks, err := b.store.ListTasks()
	if err != nil {
		b.logger.Warnf("ble: list tasks: %v", err)
		tasks = nil // push with empty tickets rather than aborting
	}

	snap, err := b.store.LatestUsageSnapshot()
	if err != nil {
		b.logger.Warnf("ble: usage snapshot: %v", err)
	}

	pct, spend := 0, 0
	resetsAt := ""
	if snap != nil {
		pct = int(math.Round(snap.FiveHourUtil))
		spend = int(math.Round(snap.SevenDayUtil))
		resetsAt = snap.FiveHourResetsAt
	}

	var tickets []bleTicket
	pending, running := 0, 0
	for _, t := range tasks {
		if excludedStatuses[t.Status] {
			continue
		}
		s := mapTicketStatus(t)
		if s == 1 {
			pending++
		}
		if s == 0 {
			running++
		}
		name := t.Title
		if len(name) > 24 {
			name = name[:24]
		}
		tickets = append(tickets, bleTicket{ID: t.DisplayID, Status: s, Name: name})
	}
	if tickets == nil {
		tickets = []bleTicket{} // send [] not null
	}

	reqCount := b.fetchPendingRequests()

	b.mu.Lock()
	p := blePayload{
		Pct:     pct,
		Spend:   spend,
		Limit:   100,
		Pending: pending,
		Running: running,
		Resets:  resetsAt,
		Req:     reqCount,
		Tickets: tickets,
		Lat:     b.lat,
		Lon:     b.lon,
	}
	if !b.locSent {
		p.LocName = b.locName
	}
	b.mu.Unlock()

	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	// Truncate tickets to fit within the BLE characteristic size limit.
	for len(data) > payloadMax && len(p.Tickets) > 0 {
		p.Tickets = p.Tickets[:len(p.Tickets)-1]
		data, _ = json.Marshal(p)
	}

	// Only add history if payload fits under 350 bytes (leaves room for BLE limit).
	if len(data) < 350 {
		hist := b.fetchUsageHistory()
		if len(hist) > 0 {
			p.Hist = hist
			data, _ = json.Marshal(p)
			// If adding history blew past the limit, drop it.
			if len(data) > payloadMax {
				p.Hist = nil
				data, _ = json.Marshal(p)
			}
		}
	}

	if _, err := char.Write(data); err != nil {
		return fmt.Errorf("ble write: %w", err)
	}

	b.mu.Lock()
	b.locSent = true
	b.mu.Unlock()

	b.logger.Debugf("ble: pushed %d bytes", len(data))
	return nil
}

// mapTicketStatus converts a task status to a BLE display code:
//
//	0 = running (blue)
//	1 = review  (orange)
//	2 = merged  (purple)
func mapTicketStatus(t domain.Task) int {
	switch t.Status {
	case "running", "in_progress":
		return 0 // blue
	case "pr_created":
		for _, pr := range t.PRs {
			if pr.PRURL != "" {
				return 2 // purple — has a PR URL, treat as merged/complete
			}
		}
		return 1 // orange — PR created but no URL yet
	default:
		return 0 // blue
	}
}

// fetchPendingRequests queries the local API for pending human requests.
func (b *Bridge) fetchPendingRequests() int {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	url := fmt.Sprintf("http://127.0.0.1:%d/api/requests", b.port)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0
	}

	var requests []struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&requests); err != nil {
		return 0
	}
	count := 0
	for _, r := range requests {
		if r.Status == "pending" {
			count++
		}
	}
	return count
}

// fetchUsageHistory fetches the last 5 hours of usage snapshots from the local
// API, samples down to at most 12 points, and returns FiveHourUtil values.
func (b *Bridge) fetchUsageHistory() []float64 {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	url := fmt.Sprintf("http://127.0.0.1:%d/api/usage/history?hours=5", b.port)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}

	var snaps []struct {
		FiveHourUtil float64 `json:"five_hour_util"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&snaps); err != nil {
		return nil
	}
	if len(snaps) == 0 {
		return nil
	}

	// Sample down to at most 12 points.
	const maxPoints = 12
	if len(snaps) <= maxPoints {
		out := make([]float64, len(snaps))
		for i, s := range snaps {
			out[i] = math.Round(s.FiveHourUtil*10) / 10
		}
		return out
	}

	out := make([]float64, 0, maxPoints)
	step := float64(len(snaps)-1) / float64(maxPoints-1)
	for i := 0; i < maxPoints; i++ {
		idx := int(math.Round(float64(i) * step))
		out = append(out, math.Round(snaps[idx].FiveHourUtil*10)/10)
	}
	return out
}

// ---------------------------------------------------------------------------
// Voice task creation (START/STOP button on ESP32)
// ---------------------------------------------------------------------------

func (b *Bridge) handleMicNotification(ctx context.Context, msg string) {
	switch strings.ToUpper(msg) {
	case "START":
		b.logger.Infof("ble: mic START received")
		b.startRecording(ctx)
	case "STOP":
		b.logger.Infof("ble: mic STOP received")
		go b.stopAndCreateTask(ctx)
	case "FOCUS":
		b.logger.Infof("ble: FOCUS received — activating window")
		exec.CommandContext(ctx, "osascript", "-e", `tell application "Burnrate" to activate`).Run()
	case "TASK_MODAL":
		b.logger.Infof("ble: TASK_MODAL received — opening voice recorder")
		exec.CommandContext(ctx, "osascript", "-e", `tell application "Burnrate" to activate`).Run()
		go b.postVoiceOpen(ctx)
	default:
		b.logger.Warnf("ble: unknown mic notification: %q", msg)
	}
}

// postVoiceOpen triggers the voice recorder modal in the desktop app via SSE.
func (b *Bridge) postVoiceOpen(ctx context.Context) {
	url := fmt.Sprintf("http://127.0.0.1:%d/api/voice/open", b.port)
	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		b.logger.Warnf("ble: voice/open: %v", err)
		return
	}
	resp.Body.Close()
}

// killStaleRecordings finds and kills any orphaned burnboi-voice recording
// processes left over from a previous daemon run.
func (b *Bridge) killStaleRecordings() {
	out, err := exec.Command("pgrep", "-f", "burnboi-voice").Output()
	if err != nil || len(out) == 0 {
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		pid, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || pid <= 0 {
			continue
		}
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Signal(syscall.SIGINT)
			b.logger.Infof("ble: killed stale recording process %d", pid)
		}
	}
}

func (b *Bridge) startRecording(ctx context.Context) {
	b.recMu.Lock()
	defer b.recMu.Unlock()

	if b.recCmd != nil {
		b.logger.Warnf("ble: recording already in progress")
		return
	}

	tmp, err := os.CreateTemp("", "burnboi-voice-*.wav")
	if err != nil {
		b.logger.Errorf("ble: create temp file: %v", err)
		return
	}
	tmp.Close()
	b.recFile = tmp.Name()

	// Try sox/rec first (sox ships the `rec` symlink), then ffmpeg.
	var cmd *exec.Cmd
	if p, err := exec.LookPath("rec"); err == nil {
		cmd = exec.Command(p, "-q", "-r", "16000", "-c", "1", b.recFile)
	} else if p, err := exec.LookPath("sox"); err == nil {
		cmd = exec.Command(p, "-d", "-q", "-r", "16000", "-c", "1", b.recFile)
	} else if p, err := exec.LookPath("ffmpeg"); err == nil {
		cmd = exec.Command(p, "-f", "avfoundation", "-i", ":0",
			"-ar", "16000", "-ac", "1", "-y", b.recFile)
	} else {
		b.logger.Errorf("ble: no audio recorder found (install sox or ffmpeg)")
		os.Remove(b.recFile)
		b.recFile = ""
		return
	}

	if err := cmd.Start(); err != nil {
		b.logger.Errorf("ble: start recording: %v", err)
		os.Remove(b.recFile)
		b.recFile = ""
		return
	}
	b.recCmd = cmd
	b.logger.Infof("ble: recording started -> %s", b.recFile)

	// Watchdog: auto-stop after maxRecordSec or on context cancellation.
	go func() {
		select {
		case <-ctx.Done():
			b.logger.Infof("ble: context canceled, stopping recording")
			b.abortRecording()
		case <-time.After(maxRecordSec * time.Second):
			b.logger.Infof("ble: recording hit %ds limit, stopping", maxRecordSec)
			b.stopAndCreateTask(ctx)
		}
	}()
}

// abortRecording stops any in-progress recording without creating a task.
func (b *Bridge) abortRecording() {
	b.recMu.Lock()
	cmd := b.recCmd
	file := b.recFile
	b.recCmd = nil
	b.recFile = ""
	b.recMu.Unlock()

	if cmd == nil {
		return
	}
	if cmd.Process != nil {
		_ = cmd.Process.Signal(syscall.SIGINT)
	}
	_ = cmd.Wait()
	if file != "" {
		os.Remove(file)
	}
}

func (b *Bridge) stopAndCreateTask(ctx context.Context) {
	b.recMu.Lock()
	cmd := b.recCmd
	file := b.recFile
	b.recCmd = nil
	b.recFile = ""
	b.recMu.Unlock()

	if cmd == nil || file == "" {
		b.logger.Warnf("ble: no recording in progress")
		return
	}

	// Gracefully stop the recorder with SIGINT so the WAV header is finalized.
	if cmd.Process != nil {
		_ = cmd.Process.Signal(syscall.SIGINT)
	}
	_ = cmd.Wait()

	defer os.Remove(file)

	b.logger.Infof("ble: recording stopped, transcribing...")

	text, err := b.transcribeAudio(ctx, file)
	if err != nil {
		b.logger.Errorf("ble: transcribe: %v", err)
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		b.logger.Warnf("ble: empty transcription")
		return
	}

	if err := b.createVoiceTask(ctx, text); err != nil {
		b.logger.Errorf("ble: create voice task: %v", err)
		return
	}
	b.logger.Infof("ble: voice task created from: %q", text)
}

// transcribeAudio POSTs the audio file to the local voice/transcribe endpoint.
func (b *Bridge) transcribeAudio(ctx context.Context, audioPath string) (string, error) {
	f, err := os.Open(audioPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("audio", "recording.wav")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, f); err != nil {
		return "", err
	}
	w.Close()

	url := fmt.Sprintf("http://127.0.0.1:%d/api/voice/transcribe", b.port)
	req, err := http.NewRequestWithContext(ctx, "POST", url, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("transcribe returned %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Text, nil
}

// createVoiceTask POSTs the transcribed text to the local voice/task endpoint.
func (b *Bridge) createVoiceTask(ctx context.Context, text string) error {
	body, _ := json.Marshal(map[string]string{"text": text})
	url := fmt.Sprintf("http://127.0.0.1:%d/api/voice/task", b.port)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("voice/task returned %d: %s", resp.StatusCode, respBody)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Location resolution
// ---------------------------------------------------------------------------

// resolveLocation tries CoreLocation, then ipinfo.io, then falls back to
// Atlanta defaults. Called once at startup.
func (b *Bridge) resolveLocation() {
	// 1. CoreLocation via Swift (may fail without macOS Location permission).
	if lat, lon, err := locationViaCoreLocation(); err == nil {
		b.mu.Lock()
		b.lat = lat
		b.lon = lon
		b.mu.Unlock()
		b.logger.Infof("ble: location via CoreLocation: %.3f, %.3f", lat, lon)
		// Still need a city name -- try ipinfo for that.
		if _, _, name, err := locationViaIPInfo(); err == nil && name != "" {
			b.mu.Lock()
			b.locName = name
			b.mu.Unlock()
		}
		return
	}

	// 2. ipinfo.io (gives coordinates and city name).
	if lat, lon, name, err := locationViaIPInfo(); err == nil {
		b.mu.Lock()
		b.lat = lat
		b.lon = lon
		if name != "" {
			b.locName = name
		}
		b.mu.Unlock()
		b.logger.Infof("ble: location via ipinfo: %.3f, %.3f (%s)", lat, lon, name)
		return
	}

	// 3. Defaults.
	b.logger.Infof("ble: using default location: %s (%.3f, %.3f)",
		defaultLocName, defaultLat, defaultLon)
}

// locationViaCoreLocation compiles and runs a small Swift program that uses
// CLLocationManager.requestLocation(). Requires macOS Location Services
// permission for the calling process.
func locationViaCoreLocation() (lat, lon float64, err error) {
	const script = `import CoreLocation
import Foundation
class D: NSObject, CLLocationManagerDelegate {
    let s = DispatchSemaphore(value: 0)
    var lat = 0.0, lon = 0.0, ok = false
    func locationManager(_ m: CLLocationManager, didUpdateLocations l: [CLLocation]) {
        if let c = l.last?.coordinate { lat = c.latitude; lon = c.longitude; ok = true }
        s.signal()
    }
    func locationManager(_ m: CLLocationManager, didFailWithError e: Error) { s.signal() }
}
let d = D(); let m = CLLocationManager()
m.delegate = d; m.desiredAccuracy = kCLLocationAccuracyKilometer; m.requestLocation()
_ = d.s.wait(timeout: .now() + 5)
if d.ok { print("\(d.lat),\(d.lon)") } else { exit(1) }
`
	tmp, err := os.CreateTemp("", "burnboi-loc-*.swift")
	if err != nil {
		return 0, 0, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(script); err != nil {
		tmp.Close()
		return 0, 0, err
	}
	tmp.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "swift", tmp.Name()).Output()
	if err != nil {
		return 0, 0, fmt.Errorf("swift location: %w", err)
	}

	parts := strings.Split(strings.TrimSpace(string(out)), ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected output: %s", out)
	}
	lat, err = strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return 0, 0, err
	}
	lon, err = strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return 0, 0, err
	}
	return lat, lon, nil
}

// locationViaIPInfo queries the ipinfo.io API for approximate coordinates and
// city name based on the host's public IP.
func locationViaIPInfo() (lat, lon float64, name string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", "https://ipinfo.io/json", nil)
	if err != nil {
		return 0, 0, "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, 0, "", err
	}
	defer resp.Body.Close()

	var info struct {
		City   string `json:"city"`
		Region string `json:"region"`
		Loc    string `json:"loc"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return 0, 0, "", err
	}

	parts := strings.Split(info.Loc, ",")
	if len(parts) != 2 {
		return 0, 0, "", fmt.Errorf("invalid loc field: %q", info.Loc)
	}
	lat, err = strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return 0, 0, "", err
	}
	lon, err = strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return 0, 0, "", err
	}

	name = info.City
	if info.Region != "" {
		name += ", " + info.Region
	}
	return lat, lon, name, nil
}

// ---------------------------------------------------------------------------
// USB serial reset (OOM recovery)
// ---------------------------------------------------------------------------

// resetViaSerial toggles DTR/RTS on the ESP32's USB serial port to trigger a
// hardware reset. This is the fallback when the device freezes (OOM) and the
// firmware watchdog hasn't fired.
func (b *Bridge) resetViaSerial() {
	matches, _ := filepath.Glob("/dev/cu.usbmodem*")
	if len(matches) == 0 {
		b.logger.Warnf("ble: no /dev/cu.usbmodem* found for serial reset")
		return
	}
	port := matches[0]
	b.logger.Infof("ble: attempting serial reset via %s", port)

	// Open+close with hupcl asserts DTR, which is wired to EN (reset) on most
	// ESP32 dev boards. stty sets the line discipline; the open/close cycle
	// (via cat with timeout) toggles the signal.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Enable hang-up-on-close (asserts DTR when the port is opened/closed)
	if err := exec.CommandContext(ctx, "stty", "-f", port, "hupcl", "115200").Run(); err != nil {
		b.logger.Warnf("ble: stty hupcl failed: %v", err)
		return
	}
	// Opening and immediately closing the port triggers the DTR toggle → EN reset
	cmd := exec.CommandContext(ctx, "bash", "-c", fmt.Sprintf("exec 3<>%s; sleep 0.1; exec 3>&-", port))
	if err := cmd.Run(); err != nil {
		b.logger.Warnf("ble: serial port toggle failed: %v", err)
		return
	}
	b.logger.Infof("ble: serial reset sent, device should reboot in ~2s")
	time.Sleep(3 * time.Second)
}

// ---------------------------------------------------------------------------
// UUID helper
// ---------------------------------------------------------------------------

// mustParseUUID parses a standard UUID string (e.g.
// "cb0a0001-5b20-4c5a-9b3d-8a7e6f1d2c3b") into a bluetooth.UUID.
// Panics on invalid input.
func mustParseUUID(s string) bluetooth.UUID {
	u, err := bluetooth.ParseUUID(s)
	if err != nil {
		panic("ble: invalid UUID: " + err.Error())
	}
	return u
}

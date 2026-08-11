package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Verhum/burnrate/internal/service"
)

func (s *Server) handleVoiceStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, s.whisper.Status())
}

func (s *Server) handleVoiceInstall(w http.ResponseWriter, r *http.Request) {
	go func() {
		if err := s.whisper.Install(); err != nil {
			s.logger.Errorf("whisper install failed: %v", err)
		}
	}()
	writeJSON(w, 202, s.whisper.Status())
}

func (s *Server) handleTranscribe(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, 400, "invalid multipart form")
		return
	}
	file, _, err := r.FormFile("audio")
	if err != nil {
		writeError(w, 400, "missing audio field")
		return
	}
	defer file.Close()

	tmp, err := os.CreateTemp("", "voice-*.webm")
	if err != nil {
		writeError(w, 500, "failed to create temp file")
		return
	}
	defer os.Remove(tmp.Name())

	if _, err := io.Copy(tmp, file); err != nil {
		tmp.Close()
		writeError(w, 500, "failed to write audio")
		return
	}
	tmp.Close()

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	text, err := s.whisper.Transcribe(ctx, tmp.Name())
	if err != nil {
		writeError(w, 500, fmt.Sprintf("transcription failed: %v", err))
		return
	}

	writeJSON(w, 200, map[string]string{"text": text})
}

func (s *Server) handleVoiceTask(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		writeError(w, 400, "empty transcription")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	title, prompt, valid := structureWithClaude(ctx, body.Text)
	if !valid {
		writeError(w, 422, "Could not find a reasonable task in the recording. Try again with a clearer description of what you want done.")
		return
	}

	task, err := s.taskSvc.CreateTask(r.Context(), service.CreateTaskInput{
		Title:  title,
		Prompt: prompt,
	})
	if err != nil {
		serviceError(w, err)
		return
	}
	s.broadcastTasks()
	writeJSON(w, 201, task)
}

func (s *Server) handleVoiceOpen(w http.ResponseWriter, _ *http.Request) {
	s.hub.broadcast("voice-open", nil)
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func structureWithClaude(ctx context.Context, transcription string) (title, prompt string, valid bool) {
	claudePath := "claude"
	for _, p := range []string{
		os.Getenv("HOME") + "/.local/bin/claude",
		"/usr/local/bin/claude",
	} {
		if _, err := os.Stat(p); err == nil {
			claudePath = p
			break
		}
	}

	metaPrompt := fmt.Sprintf(`You are a task parser for a software engineering task manager. Given a voice transcription, determine if it describes a reasonable task, and if so extract a title and prompt.

Voice transcription: "%s"

If the transcription does not contain a reasonable task (silence, gibberish, off-topic conversation, too vague to act on, or not a software/work task), respond with exactly:
VALID: no

Otherwise respond in exactly this format (no markdown, no extra text):
VALID: yes
TITLE: <concise task title, under 80 chars>
PROMPT: <detailed instructions for an AI agent to complete the task>`, transcription)

	var stdout bytes.Buffer
	cmd := exec.CommandContext(ctx, claudePath, "--print", "-p", metaPrompt)
	cmd.Stdout = &stdout
	cmd.Env = append(os.Environ(), "PATH="+os.Getenv("PATH")+":/usr/local/bin:"+os.Getenv("HOME")+"/.local/bin")

	if err := cmd.Run(); err != nil {
		return transcription, "", true
	}

	return parseClaudeOutput(stdout.String(), transcription)
}

func parseClaudeOutput(output, fallback string) (title, prompt string, valid bool) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var promptLines []string
	inPrompt := false
	valid = true
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(trimmed, "VALID:"); ok && !inPrompt {
			if strings.TrimSpace(rest) == "no" {
				return "", "", false
			}
			continue
		}
		if rest, ok := strings.CutPrefix(trimmed, "TITLE:"); ok && !inPrompt {
			title = strings.TrimSpace(rest)
			continue
		}
		if rest, ok := strings.CutPrefix(trimmed, "PROMPT:"); ok {
			inPrompt = true
			if r := strings.TrimSpace(rest); r != "" {
				promptLines = append(promptLines, r)
			}
			continue
		}
		if inPrompt {
			promptLines = append(promptLines, trimmed)
		}
	}
	if title == "" {
		title = fallback
	}
	prompt = strings.Join(promptLines, "\n")
	return title, prompt, true
}

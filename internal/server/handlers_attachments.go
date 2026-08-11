package server

import (
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Verhum/burnrate/internal/store"
)

const maxUploadSize = 10 << 20 // 10 MB

// Raster types only, and only those http.DetectContentType can positively
// identify from magic bytes. SVG is deliberately absent: it is a script carrier.
var storableImageTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
	"image/bmp":  true,
}

func (s *Server) attachmentDir(taskID int64) string {
	return filepath.Join(s.cfg.DataDir, "attachments", fmt.Sprintf("task-%d", taskID))
}

func (s *Server) handleListAttachments(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, 400, "invalid task id")
		return
	}
	attachments, err := s.st.ListAttachments(id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if attachments == nil {
		attachments = []store.Attachment{}
	}
	writeJSON(w, 200, attachments)
}

func (s *Server) handleUploadAttachment(w http.ResponseWriter, r *http.Request) {
	taskID, err := pathID(r)
	if err != nil {
		writeError(w, 400, "invalid task id")
		return
	}
	if _, err := s.st.GetTask(taskID); err != nil {
		writeError(w, 404, "task not found")
		return
	}

	ct := r.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		writeError(w, 400, "expected multipart/form-data")
		return
	}

	reader := multipart.NewReader(r.Body, params["boundary"])
	part, err := reader.NextPart()
	if err != nil {
		writeError(w, 400, "no file in upload")
		return
	}
	defer part.Close()

	filename := part.FileName()
	if filename == "" {
		filename = "image.png"
	}
	filename = filepath.Base(filename)

	data, err := io.ReadAll(io.LimitReader(part, maxUploadSize+1))
	if err != nil {
		writeError(w, 500, "failed to read upload")
		return
	}
	if int64(len(data)) > maxUploadSize {
		writeError(w, 413, "file too large (max 10 MB)")
		return
	}

	// Sniff rather than trust the client's declared type, and match an exact
	// allowlist rather than an "image/" prefix. The prefix let image/svg+xml
	// through, and an SVG served back from this origin is script execution
	// against the API it is stored in.
	contentType := http.DetectContentType(data)
	if !storableImageTypes[contentType] {
		writeError(w, 400, "only PNG, JPEG, GIF, WebP, and BMP images are supported")
		return
	}

	dir := s.attachmentDir(taskID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		writeError(w, 500, "failed to create attachment directory")
		return
	}

	att, err := s.st.AddAttachment(taskID, filename, contentType, int64(len(data)))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}

	storedName := fmt.Sprintf("%d-%s", att.ID, filename)
	if err := os.WriteFile(filepath.Join(dir, storedName), data, 0644); err != nil {
		s.st.DeleteAttachment(att.ID)
		writeError(w, 500, "failed to write file")
		return
	}

	writeJSON(w, 201, att)
}

func (s *Server) handleServeAttachment(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, 400, "invalid attachment id")
		return
	}
	att, err := s.st.GetAttachment(id)
	if err != nil {
		writeError(w, 404, "attachment not found")
		return
	}

	storedName := fmt.Sprintf("%d-%s", att.ID, att.Filename)
	filePath := filepath.Join(s.attachmentDir(att.TaskID), storedName)
	f, err := os.Open(filePath)
	if err != nil {
		writeError(w, 404, "attachment file not found")
		return
	}
	defer f.Close()

	// Rows predating the upload allowlist may hold any declared type, so clamp
	// here too and let nosniff stop the browser from second-guessing us.
	contentType := att.ContentType
	if !storableImageTypes[contentType] {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	io.Copy(w, f)
}

func (s *Server) handleDeleteAttachment(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, 400, "invalid attachment id")
		return
	}
	att, err := s.st.GetAttachment(id)
	if err != nil {
		writeError(w, 404, "attachment not found")
		return
	}

	storedName := fmt.Sprintf("%d-%s", att.ID, att.Filename)
	os.Remove(filepath.Join(s.attachmentDir(att.TaskID), storedName))

	if err := s.st.DeleteAttachment(id); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"status": "deleted"})
}

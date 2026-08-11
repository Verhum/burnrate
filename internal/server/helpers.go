package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Verhum/burnrate/internal/service"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

func serviceError(w http.ResponseWriter, err error) {
	var ve *service.ValidationError
	var nfe *service.NotFoundError
	var ce *service.ConflictError
	switch {
	case errors.As(err, &ve):
		writeError(w, 400, ve.Error())
	case errors.As(err, &nfe):
		writeError(w, 404, nfe.Error())
	case errors.As(err, &ce):
		writeError(w, 409, ce.Error())
	default:
		writeError(w, 500, err.Error())
	}
}

func compactInput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) == nil {
		parts := make([]string, 0, len(m))
		for k, v := range m {
			s := fmt.Sprintf("%v", v)
			if len(s) > 80 {
				s = s[:80] + "..."
			}
			parts = append(parts, k+"="+s)
		}
		return strings.Join(parts, " ")
	}
	s := string(raw)
	if len(s) > 120 {
		s = s[:120] + "..."
	}
	return s
}

func truncateContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if len(s) > 200 {
			return s[:200] + "..."
		}
		return s
	}
	str := string(raw)
	if len(str) > 200 {
		return str[:200] + "..."
	}
	return str
}

package server

import (
	"net/http"

	"github.com/Verhum/burnrate/internal/domain"
)

func (s *Server) handleListModels(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, domain.AvailableModels)
}

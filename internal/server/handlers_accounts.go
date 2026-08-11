package server

import (
	"encoding/json"
	"net/http"

	"github.com/Verhum/burnrate/internal/config"
	"github.com/Verhum/burnrate/internal/usage"
)

type accountView struct {
	config.Account
	Active bool `json:"active"`
}

func (s *Server) handleListAccounts(w http.ResponseWriter, _ *http.Request) {
	active := s.sched.ActiveConfigDir()

	views := []accountView{{
		Account: config.Account{ConfigDir: "", Label: "inherited environment", Source: "inherited"},
		Active:  active == "",
	}}

	activeKnown := active == ""
	for _, a := range config.DiscoverAccounts() {
		if a.ConfigDir == active {
			activeKnown = true
		}
		views = append(views, accountView{Account: a, Active: a.ConfigDir == active})
	}

	if !activeKnown {
		views = append(views, accountView{
			Account: config.Account{
				ConfigDir:      active,
				Label:          active + " (pinned)",
				Source:         "custom",
				KeychainSuffix: usage.ConfigDirSuffix(active),
			},
			Active: true,
		})
	}

	writeJSON(w, 200, map[string]any{"active_config_dir": active, "accounts": views})
}

func (s *Server) handleSelectAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ConfigDir string `json:"config_dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}

	configDir, keychain, pwFile := "", "", ""
	if body.ConfigDir != "" {
		var match *config.Account
		for _, a := range config.DiscoverAccounts() {
			if a.ConfigDir == body.ConfigDir {
				a := a
				match = &a
				break
			}
		}
		if match == nil {
			writeError(w, 400, "unknown account (not among discovered accounts)")
			return
		}
		configDir, keychain, pwFile = match.ConfigDir, match.SandboxKeychain, match.SandboxPasswordFile
	}

	for k, v := range map[string]string{
		"claude_config_dir":              configDir,
		"sandbox_keychain":               keychain,
		"sandbox_keychain_password_file": pwFile,
	} {
		if err := s.st.SetSetting(k, v); err != nil {
			writeError(w, 500, err.Error())
			return
		}
	}

	s.sched.SetAccount(configDir, keychain, pwFile)
	s.hub.broadcast("status", s.statusPayload())
	writeJSON(w, 200, map[string]string{"status": "selected", "config_dir": configDir})
}

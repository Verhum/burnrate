package service

import (
	"context"

	"github.com/Verhum/burnrate/internal/domain"
)

type AccountDiscoverer interface {
	DiscoverAccounts() []AccountInfo
}

type AccountInfo struct {
	ConfigDir           string
	Label               string
	Source              string
	SandboxKeychain     string
	SandboxPasswordFile string
	KeychainSuffix      string
}

type AccountSwitcher interface {
	ActiveConfigDir() string
	SetAccount(configDir, sandboxKeychain, sandboxKeychainPasswordFile string)
}

type AccountService struct {
	settings   domain.SettingsRepository
	switcher   AccountSwitcher
	discoverer AccountDiscoverer
}

func NewAccountService(
	settings domain.SettingsRepository,
	switcher AccountSwitcher,
	discoverer AccountDiscoverer,
) *AccountService {
	return &AccountService{settings: settings, switcher: switcher, discoverer: discoverer}
}

type AccountView struct {
	ConfigDir           string `json:"config_dir"`
	Label               string `json:"label"`
	Source              string `json:"source"`
	SandboxKeychain     string `json:"sandbox_keychain,omitempty"`
	SandboxPasswordFile string `json:"sandbox_password_file,omitempty"`
	KeychainSuffix      string `json:"keychain_suffix,omitempty"`
	Active              bool   `json:"active"`
}

func (s *AccountService) ListAccounts(ctx context.Context) (string, []AccountView) {
	active := s.switcher.ActiveConfigDir()

	views := []AccountView{{
		ConfigDir: "",
		Label:     "inherited environment",
		Source:    "inherited",
		Active:    active == "",
	}}

	activeKnown := active == ""
	for _, a := range s.discoverer.DiscoverAccounts() {
		if a.ConfigDir == active {
			activeKnown = true
		}
		views = append(views, AccountView{
			ConfigDir:       a.ConfigDir,
			Label:           a.Label,
			Source:          a.Source,
			SandboxKeychain: a.SandboxKeychain,
			KeychainSuffix:  a.KeychainSuffix,
			Active:          a.ConfigDir == active,
		})
	}

	if !activeKnown {
		views = append(views, AccountView{
			ConfigDir: active,
			Label:     active + " (pinned)",
			Source:    "custom",
			Active:    true,
		})
	}

	return active, views
}

func (s *AccountService) SelectAccount(ctx context.Context, configDir string) error {
	selectedDir, keychain, pwFile := "", "", ""
	if configDir != "" {
		var match *AccountInfo
		for _, a := range s.discoverer.DiscoverAccounts() {
			if a.ConfigDir == configDir {
				a := a
				match = &a
				break
			}
		}
		if match == nil {
			return &ValidationError{Field: "config_dir", Message: "unknown account (not among discovered accounts)"}
		}
		selectedDir, keychain, pwFile = match.ConfigDir, match.SandboxKeychain, match.SandboxPasswordFile
	}

	for k, v := range map[string]string{
		"claude_config_dir":              selectedDir,
		"sandbox_keychain":               keychain,
		"sandbox_keychain_password_file": pwFile,
	} {
		if err := s.settings.SetSetting(k, v); err != nil {
			return err
		}
	}

	s.switcher.SetAccount(selectedDir, keychain, pwFile)
	return nil
}

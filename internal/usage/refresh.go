package usage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	refreshMargin = 5 * time.Minute
	oauthTokenURL = "https://console.anthropic.com/v1/oauth/token"
	oauthClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
)

var (
	refreshMu      sync.Mutex
	refreshHTTP    *http.Client = http.DefaultClient
	lastRefreshAt  time.Time
	lastRefreshTok string
)

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

func ExpiresTime(b *TokenBundle) time.Time {
	if b.ExpiresAt <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(b.ExpiresAt)
}

func EnsureFresh(b *TokenBundle) (accessToken string, refreshed bool, err error) {
	if b == nil {
		return "", false, fmt.Errorf("nil token bundle")
	}

	exp := ExpiresTime(b)
	if !exp.IsZero() && time.Until(exp) > refreshMargin {
		return b.AccessToken, false, nil
	}

	if b.RefreshToken == "" {
		return b.AccessToken, false, nil
	}

	refreshMu.Lock()
	defer refreshMu.Unlock()

	// Single-flight: if we just refreshed for this same bundle (within the last
	// 30 seconds), return the cached result instead of double-refreshing.
	if !lastRefreshAt.IsZero() && time.Since(lastRefreshAt) < 30*time.Second && lastRefreshTok != "" {
		return lastRefreshTok, true, nil
	}

	newAccess, newRefresh, expiresIn, err := doRefresh(b.RefreshToken)
	if err != nil {
		return "", false, fmt.Errorf("oauth refresh: %w", err)
	}

	newExpiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second).UnixMilli()

	newJSON, err := buildUpdatedJSON(b.RawJSON, newAccess, newRefresh, newExpiresAt)
	if err != nil {
		return "", false, fmt.Errorf("build updated credentials: %w", err)
	}

	// CRITICAL: persist BEFORE returning the new token.
	if err := persistBundle(b, newJSON); err != nil {
		return "", false, fmt.Errorf("persist refreshed token (new token NOT used): %w", err)
	}

	if err := VerifyWriteBack(b, newAccess); err != nil {
		return "", false, fmt.Errorf("verify refreshed token (new token NOT used): %w", err)
	}

	lastRefreshAt = time.Now()
	lastRefreshTok = newAccess

	b.AccessToken = newAccess
	b.RefreshToken = newRefresh
	b.ExpiresAt = newExpiresAt
	b.RawJSON = newJSON

	return newAccess, true, nil
}

func doRefresh(refreshToken string) (accessToken, newRefreshToken string, expiresIn int, err error) {
	payload, _ := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     oauthClientID,
	})

	req, err := http.NewRequest("POST", oauthTokenURL, bytes.NewReader(payload))
	if err != nil {
		return "", "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := refreshHTTP.Do(req)
	if err != nil {
		return "", "", 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", 0, err
	}

	if resp.StatusCode != 200 {
		return "", "", 0, fmt.Errorf("refresh returned %d: %s", resp.StatusCode, string(body))
	}

	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", "", 0, fmt.Errorf("parse refresh response: %w", err)
	}
	if tok.AccessToken == "" {
		return "", "", 0, fmt.Errorf("refresh response missing access_token")
	}

	return tok.AccessToken, tok.RefreshToken, tok.ExpiresIn, nil
}

func buildUpdatedJSON(originalJSON, newAccess, newRefresh string, newExpiresAt int64) (string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(originalJSON), &raw); err != nil {
		return "", err
	}

	var oauth map[string]json.RawMessage
	if err := json.Unmarshal(raw["claudeAiOauth"], &oauth); err != nil {
		return "", err
	}

	accessBytes, _ := json.Marshal(newAccess)
	oauth["accessToken"] = accessBytes

	if newRefresh != "" {
		refreshBytes, _ := json.Marshal(newRefresh)
		oauth["refreshToken"] = refreshBytes
	}

	expiresBytes, _ := json.Marshal(newExpiresAt)
	oauth["expiresAt"] = expiresBytes

	updatedOAuth, err := json.Marshal(oauth)
	if err != nil {
		return "", err
	}
	raw["claudeAiOauth"] = updatedOAuth

	result, err := json.Marshal(raw)
	if err != nil {
		return "", err
	}
	return string(result), nil
}

func persistBundle(b *TokenBundle, newJSON string) error {
	switch b.Source {
	case "keychain":
		return WriteBackKeychain(b, newJSON)
	case "file":
		return WriteBackFile(b, newJSON)
	default:
		return fmt.Errorf("unknown source %q for write-back", b.Source)
	}
}

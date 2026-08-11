package usage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Identifies burnrate as a third party in the usage request's User-Agent. burnrate
// is not affiliated with or endorsed by Anthropic.
const burnrateUserAgent = "burnrate (+https://github.com/Verhum/burnrate)"

var ErrRateLimited429 = errors.New("usage API rate limited (429)")

type ErrRateLimited struct {
	RetryAfter time.Duration
}

func (e *ErrRateLimited) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("usage API rate limited (429), retry after %s", e.RetryAfter)
	}
	return ErrRateLimited429.Error()
}

func (e *ErrRateLimited) Is(target error) bool {
	return target == ErrRateLimited429
}

func parseRetryAfter(resp *http.Response) time.Duration {
	h := resp.Header.Get("Retry-After")
	if h == "" {
		return 0
	}
	if secs, err := strconv.Atoi(h); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil {
		d := time.Until(t)
		if d > 0 {
			return d
		}
	}
	return 0
}

type Client struct {
	URL        string
	HTTPClient *http.Client

	ClaudeConfigDir             string
	SandboxKeychain             string
	SandboxKeychainPasswordFile string

	mu             sync.Mutex
	cachedToken    string
	cachedVersion  string
	versionFetched bool
}

func NewClient(url string) *Client {
	return &Client{
		URL:        url,
		HTTPClient: http.DefaultClient,
	}
}

// SetAccount switches the account the client reads its token from and clears
// the cached token + claude version so the next Fetch re-resolves under the new
// account. Safe to call while the scheduler is polling.
func (c *Client) SetAccount(configDir, sandboxKeychain, sandboxKeychainPasswordFile string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ClaudeConfigDir = configDir
	c.SandboxKeychain = sandboxKeychain
	c.SandboxKeychainPasswordFile = sandboxKeychainPasswordFile
	c.cachedToken = ""
	c.cachedVersion = ""
	c.versionFetched = false
}

func (c *Client) Fetch(ctx context.Context) (Snapshot, error) {
	tok, err := c.getToken(false)
	if err != nil {
		return Snapshot{}, fmt.Errorf("get token: %w", err)
	}

	snap, err := c.doFetch(ctx, tok)
	if err != nil && isUnauthorized(err) {
		ResetDiscoveredService()
		tok, err = c.getToken(true)
		if err != nil {
			return Snapshot{}, fmt.Errorf("refresh token: %w", err)
		}
		return c.doFetch(ctx, tok)
	}
	return snap, err
}

func (c *Client) doFetch(ctx context.Context, token string) (Snapshot, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.URL, nil)
	if err != nil {
		return Snapshot{}, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("Content-Type", "application/json")
	// The endpoint is an undocumented OAuth usage API that expects Claude Code, so
	// the claude-code product token has to stay first or the request is refused.
	// Appending burnrate keeps the request honest about who is actually calling:
	// a third-party tool reading the quota of the CLI installed on this machine.
	req.Header.Set("User-Agent", "claude-code/"+c.claudeVersion()+" "+burnrateUserAgent)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return Snapshot{}, fmt.Errorf("usage request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == 401 {
		return Snapshot{}, &errUnauthorized{status: resp.StatusCode}
	}
	if resp.StatusCode == 429 {
		return Snapshot{}, &ErrRateLimited{RetryAfter: parseRetryAfter(resp)}
	}
	if resp.StatusCode != 200 {
		return Snapshot{}, fmt.Errorf("usage API returned %d: %s", resp.StatusCode, string(body))
	}

	return parseAPIResponse(body)
}

func (c *Client) getToken(forceRefresh bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !forceRefresh && c.cachedToken != "" {
		return c.cachedToken, nil
	}

	if tok := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN"); tok != "" {
		c.cachedToken = tok
		return tok, nil
	}

	b, err := BundleForAccount(c.ClaudeConfigDir, c.SandboxKeychain, c.SandboxKeychainPasswordFile)
	if err != nil {
		return "", err
	}

	tok, _, err := EnsureFresh(b)
	if err != nil {
		return "", err
	}
	c.cachedToken = tok
	return tok, nil
}

func (c *Client) claudeVersion() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.versionFetched {
		return c.cachedVersion
	}
	c.versionFetched = true
	c.cachedVersion = "2.0.0"

	cmd := exec.Command("claude", "--version")
	if c.ClaudeConfigDir != "" {
		cmd.Env = append(os.Environ(), "CLAUDE_CONFIG_DIR="+c.ClaudeConfigDir)
	}
	out, err := cmd.Output()
	if err != nil {
		return c.cachedVersion
	}

	re := regexp.MustCompile(`(\d+\.\d+\.\d+)`)
	if m := re.FindString(strings.TrimSpace(string(out))); m != "" {
		c.cachedVersion = m
	}
	return c.cachedVersion
}

type errUnauthorized struct {
	status int
}

func (e *errUnauthorized) Error() string {
	return fmt.Sprintf("unauthorized (%d)", e.status)
}

func isUnauthorized(err error) bool {
	var ue *errUnauthorized
	return errors.As(err, &ue)
}

func RetryAfterFrom(err error) time.Duration {
	var rl *ErrRateLimited
	if errors.As(err, &rl) {
		return rl.RetryAfter
	}
	return 0
}

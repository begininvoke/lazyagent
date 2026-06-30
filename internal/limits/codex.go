package limits

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// defaultCodexUsageURL is the endpoint the Codex CLI's TUI polls (~every 60s via
// ChatWidget::prefetch_rate_limits) to show live rate-limit usage. It's the same
// data the official client displays, so it's always current — unlike the session
// rollouts, which only update when a turn completes.
const defaultCodexUsageURL = "https://chatgpt.com/backend-api/wham/usage"

// codexAuth is the subset of ~/.codex/auth.json we need: the ChatGPT OAuth
// access token and the account id the usage endpoint expects as a header.
type codexAuth struct {
	Tokens struct {
		AccessToken string `json:"access_token"`
		AccountID   string `json:"account_id"`
	} `json:"tokens"`
}

// readCodexAuth returns the bearer token and account id in this priority order:
//  1. CODEX_OAUTH_TOKEN env var (override for CI / debugging; CODEX_ACCOUNT_ID optional)
//  2. ~/.codex/auth.json (where the Codex CLI persists its ChatGPT login)
//
// Both "not installed" and "not logged in" surface as errAgentNotInstalled so the
// dispatcher can silently skip Codex in --agent all mode.
func readCodexAuth() (token, accountID string, err error) {
	if v := os.Getenv("CODEX_OAUTH_TOKEN"); v != "" {
		return v, os.Getenv("CODEX_ACCOUNT_ID"), nil
	}
	path := codexAuthPath()
	if path == "" {
		return "", "", errAgentNotInstalled
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", errAgentNotInstalled
		}
		return "", "", err
	}
	return readCodexAuthFromBytes(data)
}

func readCodexAuthFromBytes(data []byte) (token, accountID string, err error) {
	var a codexAuth
	if err := json.Unmarshal(data, &a); err != nil {
		return "", "", fmt.Errorf("parse Codex auth.json: %w", err)
	}
	if a.Tokens.AccessToken == "" {
		return "", "", errAgentNotInstalled
	}
	return a.Tokens.AccessToken, a.Tokens.AccountID, nil
}

func codexAuthPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex", "auth.json")
}

// codexUsageResponse is the subset of GET /backend-api/wham/usage we render.
// Fields we don't use (credits, spend_control, additional_rate_limits, …) are
// intentionally omitted.
type codexUsageResponse struct {
	PlanType  string            `json:"plan_type"`
	RateLimit *codexUsageLimits `json:"rate_limit"`
}

type codexUsageLimits struct {
	Primary   *codexUsageWindow `json:"primary_window"`
	Secondary *codexUsageWindow `json:"secondary_window"`
}

type codexUsageWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds int64   `json:"limit_window_seconds"`
	ResetAfterSeconds  int64   `json:"reset_after_seconds"`
	ResetAt            int64   `json:"reset_at"` // unix seconds
}

func fetchCodexReport(ctx context.Context) (Report, error) {
	token, accountID, err := readCodexAuth()
	if err != nil {
		if errors.Is(err, errAgentNotInstalled) {
			return Report{}, err
		}
		return Report{}, fmt.Errorf("read Codex auth: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, codexUsageURL(), nil)
	if err != nil {
		return Report{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if accountID != "" {
		req.Header.Set("chatgpt-account-id", accountID)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent())
	req.Header.Set("originator", "codex_cli_rs")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Report{}, fmt.Errorf("call Codex usage endpoint: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	switch resp.StatusCode {
	case http.StatusOK:
		// fall through to parse
	case http.StatusUnauthorized, http.StatusForbidden:
		return Report{}, fmt.Errorf("Codex OAuth token rejected (%d). It may have expired — open the Codex CLI again to refresh your login", resp.StatusCode)
	case http.StatusTooManyRequests:
		return Report{}, fmt.Errorf("Codex usage endpoint rate-limited (429). Try again in a minute")
	default:
		return Report{}, fmt.Errorf("Codex usage endpoint: %s — %s", resp.Status, snippet(body, 200))
	}

	usage, err := parseCodexUsage(body)
	if err != nil {
		return Report{}, err
	}
	report := codexUsageToReport(usage)
	if len(report.Windows) == 0 {
		return Report{}, fmt.Errorf("Codex usage endpoint returned no usable windows (response: %s)", snippet(body, 200))
	}
	return report, nil
}

func codexUsageURL() string {
	if v := os.Getenv("CODEX_USAGE_URL"); v != "" {
		return v
	}
	return defaultCodexUsageURL
}

func parseCodexUsage(data []byte) (*codexUsageResponse, error) {
	var resp codexUsageResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse Codex usage response: %w", err)
	}
	return &resp, nil
}

func codexUsageToReport(resp *codexUsageResponse) Report {
	report := Report{
		Provider: "Codex",
		Source:   codexSource(resp),
		Note:     "Note: reads /backend-api/wham/usage, the endpoint Codex CLI polls for its rate-limit display. May break or be revoked by OpenAI without notice.",
	}
	if resp == nil || resp.RateLimit == nil {
		return report
	}
	if w, ok := codexUsageWindowToWindow(resp.RateLimit.Primary); ok {
		report.Windows = append(report.Windows, w)
	}
	if w, ok := codexUsageWindowToWindow(resp.RateLimit.Secondary); ok {
		report.Windows = append(report.Windows, w)
	}
	return report
}

func codexUsageWindowToWindow(w *codexUsageWindow) (Window, bool) {
	if w == nil || w.LimitWindowSeconds <= 0 {
		return Window{}, false
	}
	minutes := int(w.LimitWindowSeconds / 60)
	var reset time.Time
	switch {
	case w.ResetAt > 0:
		reset = time.Unix(w.ResetAt, 0)
	case w.ResetAfterSeconds > 0:
		reset = time.Now().Add(time.Duration(w.ResetAfterSeconds) * time.Second)
	}
	return Window{
		Label:         codexWindowLabel(minutes),
		WindowMinutes: minutes,
		UsedPercent:   w.UsedPercent,
		ResetsAt:      reset,
	}, true
}

// codexWindowLabel names a window from its length so the renderer's window
// matchers (5-hour, 7-day/weekly) line up, while staying readable if OpenAI ever
// changes the window sizes.
func codexWindowLabel(minutes int) string {
	switch minutes {
	case 5 * 60:
		return "5-hour"
	case 7 * 24 * 60:
		return "7-day"
	}
	switch {
	case minutes > 0 && minutes%(24*60) == 0:
		return fmt.Sprintf("%d-day", minutes/(24*60))
	case minutes > 0 && minutes%60 == 0:
		return fmt.Sprintf("%d-hour", minutes/60)
	case minutes > 0:
		return fmt.Sprintf("%d-minute", minutes)
	default:
		return "window"
	}
}

func codexSource(resp *codexUsageResponse) string {
	if resp != nil && resp.PlanType != "" {
		return fmt.Sprintf("Source: Codex (ChatGPT %s) /backend-api/wham/usage", resp.PlanType)
	}
	return "Source: Codex /backend-api/wham/usage"
}

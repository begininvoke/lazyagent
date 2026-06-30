package limits

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/illegalstudio/lazyagent/internal/cursor"
)

// Cursor splits spend into two pools: an "Auto / Composer" pool (effectively
// unlimited on paid plans, billed at flat rates, does NOT draw from the included
// credit) and an "API / usage-based" pool (specific models, drawn from the plan's
// included credit — $20 Pro, $70 Pro+, $400 Ultra — then on-demand above it).
//
// lazyagent reports only the API pool, since that's the one with a meaningful
// budget to pace against. The split is keyed off the `tier` field returned by
// /api/dashboard/get-aggregated-usage-events: tier 2 is Auto/Composer, everything
// else is metered API usage.
const cursorAutoTier = 2

// cursorIncludedByPlan maps Cursor's stripeMembershipType to the dollar amount of
// API usage included in the plan. Cursor doesn't expose this per-user, so we map
// it from the plan (the same way Cursor's own UI does). Values can change; the
// CURSOR_INCLUDED_USD env var overrides them and covers plans not listed here
// (e.g. business / enterprise).
var cursorIncludedByPlan = map[string]float64{
	"free":     0,
	"pro":      20,
	"pro_plus": 70,
	"ultra":    400,
}

type cursorUsageResponse struct {
	StartOfMonth string `json:"startOfMonth"`
}

type cursorAggResponse struct {
	Aggregations   []cursorAgg `json:"aggregations"`
	TotalCostCents float64     `json:"totalCostCents"`
}

type cursorAgg struct {
	ModelIntent string  `json:"modelIntent"`
	TotalCents  float64 `json:"totalCents"`
	Tier        int     `json:"tier"`
}

// cursorUsage is the normalized input to the report builder, kept separate from
// the wire types so the mapping is pure and unit-testable.
type cursorUsage struct {
	APISpendUSD  float64
	AutoSpendUSD float64
	IncludedUSD  float64
	Plan         string
	CycleStart   time.Time
	CycleEnd     time.Time
}

func fetchCursorReport(ctx context.Context) (Report, error) {
	token, membership, ok, err := cursor.ReadAuth()
	if err != nil {
		return Report{}, fmt.Errorf("read Cursor credentials: %w", err)
	}
	if !ok {
		return Report{}, errAgentNotInstalled
	}

	userID, err := cursorUserIDFromToken(token)
	if err != nil {
		return Report{}, fmt.Errorf("decode Cursor access token: %w", err)
	}
	cookie := userID + "%3A%3A" + token

	included, plan, err := cursorIncluded(membership)
	if err != nil {
		return Report{}, err
	}

	// 1. Anchor the billing cycle on startOfMonth from /api/usage.
	var usage cursorUsageResponse
	if err := cursorGET(ctx, "https://cursor.com/api/usage?user="+userID, cookie, &usage); err != nil {
		return Report{}, err
	}
	cycleStart, err := time.Parse(time.RFC3339, usage.StartOfMonth)
	if err != nil {
		return Report{}, fmt.Errorf("parse Cursor billing cycle start %q: %w", usage.StartOfMonth, err)
	}
	cycleStart, cycleEnd := cursorBillingCycle(cycleStart)

	// 2. Sum spend over the current cycle, split into API vs Auto pools.
	now := time.Now()
	body := map[string]any{
		"teamId":    -1,
		"startDate": strconv.FormatInt(cycleStart.UnixMilli(), 10),
		"endDate":   strconv.FormatInt(now.UnixMilli(), 10),
	}
	var agg cursorAggResponse
	if err := cursorPOST(ctx, "https://cursor.com/api/dashboard/get-aggregated-usage-events", cookie, body, &agg); err != nil {
		return Report{}, err
	}
	apiCents, autoCents := cursorSpendByPool(&agg)

	return cursorUsageToReport(cursorUsage{
		APISpendUSD:  apiCents / 100,
		AutoSpendUSD: autoCents / 100,
		IncludedUSD:  included,
		Plan:         plan,
		CycleStart:   cycleStart,
		CycleEnd:     cycleEnd,
	}), nil
}

// cursorUserIDFromToken extracts the user id from the JWT access token. Cursor
// builds its session cookie as "<userId>%3A%3A<token>", where userId is the part
// of the JWT `sub` claim after the "auth0|" / "workos|" prefix.
func cursorUserIDFromToken(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("not a JWT (expected 3 dot-separated segments)")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode JWT payload: %w", err)
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("parse JWT payload: %w", err)
	}
	if claims.Sub == "" {
		return "", fmt.Errorf("JWT has no sub claim")
	}
	if i := strings.Index(claims.Sub, "|"); i >= 0 {
		return claims.Sub[i+1:], nil
	}
	return claims.Sub, nil
}

// cursorSpendByPool splits the aggregated spend into the API (metered) pool and
// the Auto/Composer (tier-2) pool. Returned values are in cents.
func cursorSpendByPool(resp *cursorAggResponse) (apiCents, autoCents float64) {
	for _, a := range resp.Aggregations {
		if a.Tier == cursorAutoTier {
			autoCents += a.TotalCents
		} else {
			apiCents += a.TotalCents
		}
	}
	return apiCents, autoCents
}

// cursorIncludedUSD returns the plan's included API-usage dollar amount and
// whether the plan is known.
func cursorIncludedUSD(membership string) (float64, bool) {
	v, ok := cursorIncludedByPlan[strings.ToLower(strings.TrimSpace(membership))]
	return v, ok
}

// cursorIncluded resolves the included API budget, preferring the
// CURSOR_INCLUDED_USD override and falling back to the per-plan table. It also
// returns a display name for the plan.
//
// When no positive budget can be determined — an unmapped plan (business /
// enterprise / trial), an empty membership, or a plan with no API budget like
// Free — it returns errAgentUnavailable wrapped with an actionable message,
// rather than a hard error. That lets `--agent all` skip Cursor silently for
// such users instead of failing the whole command.
func cursorIncluded(membership string) (usd float64, plan string, err error) {
	plan = cursorPlanName(membership)
	if v := os.Getenv("CURSOR_INCLUDED_USD"); v != "" {
		parsed, perr := strconv.ParseFloat(v, 64)
		if perr != nil {
			return 0, plan, fmt.Errorf("invalid CURSOR_INCLUDED_USD %q: %w", v, perr)
		}
		if parsed <= 0 {
			return 0, plan, fmt.Errorf("CURSOR_INCLUDED_USD must be a positive dollar amount, got %q", v)
		}
		return parsed, plan, nil
	}
	if usd, ok := cursorIncludedUSD(membership); ok && usd > 0 {
		return usd, plan, nil
	}
	return 0, plan, fmt.Errorf("%w: Cursor is signed in (plan %q) but its included API budget is unknown — set CURSOR_INCLUDED_USD to your plan's included usage (e.g. 400 for Ultra)", errAgentUnavailable, plan)
}

func cursorPlanName(membership string) string {
	m := strings.TrimSpace(membership)
	if m == "" {
		return "Cursor"
	}
	switch strings.ToLower(m) {
	case "pro_plus":
		return "Pro+"
	default:
		return strings.ToUpper(m[:1]) + m[1:]
	}
}

// cursorBillingCycle returns the [start, end) of the billing cycle anchored at
// start, where end is one calendar month later. When the next month is shorter
// (e.g. Jan 31 -> Feb), the end is clamped to that month's last day rather than
// overflowing into the following month.
func cursorBillingCycle(start time.Time) (time.Time, time.Time) {
	end := start.AddDate(0, 1, 0)
	if end.Day() != start.Day() {
		// AddDate overflowed (e.g. Jan 31 -> Mar 3); back up to the last day of
		// the intended month.
		end = end.AddDate(0, 0, -end.Day())
	}
	return start, end
}

func cursorUsageToReport(u cursorUsage) Report {
	windowMinutes := int(u.CycleEnd.Sub(u.CycleStart).Minutes())
	var usedPercent float64
	if u.IncludedUSD > 0 {
		usedPercent = 100 * u.APISpendUSD / u.IncludedUSD
	}
	return Report{
		Provider: "Cursor",
		Source: fmt.Sprintf("Source: $%.2f of $%.0f included API usage (%s); Auto/Composer $%.2f not counted",
			u.APISpendUSD, u.IncludedUSD, u.Plan, u.AutoSpendUSD),
		Windows: []Window{{
			Label:         "monthly",
			WindowMinutes: windowMinutes,
			UsedPercent:   usedPercent,
			ResetsAt:      u.CycleEnd,
		}},
		Note: "Note: reads /api/dashboard/get-aggregated-usage-events with the Cursor session token from state.vscdb — the API (usage-based) pool only, not Auto. Undocumented; may break or be revoked by Cursor without notice.",
	}
}

func cursorGET(ctx context.Context, url, cookie string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	return cursorDo(req, cookie, out)
}

func cursorPOST(ctx context.Context, url, cookie string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return cursorDo(req, cookie, out)
}

func cursorDo(req *http.Request, cookie string, out any) error {
	req.Header.Set("Cookie", "WorkosCursorSessionToken="+cookie)
	req.Header.Set("Origin", "https://cursor.com")
	req.Header.Set("Referer", "https://cursor.com/dashboard")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent())

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("call Cursor %s: %w", req.URL.Path, err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("Cursor session token rejected (%d). Open Cursor and sign in again", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Cursor %s: %s — %s", req.URL.Path, resp.Status, snippet(data, 200))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("parse Cursor %s response: %w (body: %s)", req.URL.Path, err, snippet(data, 200))
	}
	return nil
}

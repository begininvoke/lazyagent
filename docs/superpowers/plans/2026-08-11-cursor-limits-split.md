# Cursor limits split — `Cursor Models` + `Cursor API` — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Report Cursor's two metered pools as two rows in `lazyagent limits` — `Cursor Models` (Auto/Composer) and `Cursor API` (usage-based) — sourced from Cursor's own per-pool percentages instead of a hardcoded plan table.

**Architecture:** Replace the current two Cursor HTTP calls with a single `GET https://cursor.com/api/usage-summary`, which returns the billing cycle bounds and `autoPercentUsed` / `apiPercentUsed` directly. The limits dispatcher changes from one report per agent to a slice of reports per agent, so Cursor can emit two rows from one fetch while every other provider wraps its single report unchanged.

**Tech Stack:** Go 1.25.5, stdlib only (`net/http`, `encoding/json`, `time`). No new dependencies. Tests are stdlib `testing`, table-driven, with JSON fixtures under `internal/limits/testdata/`.

**Spec:** `docs/superpowers/specs/2026-08-11-cursor-limits-split-design.md`

## Global Constraints

- Provider strings are exactly `"Cursor Models"` and `"Cursor API"`, in that order.
- Window `Label` is exactly `"monthly"` for both rows (this is what routes them into the summary table's Week/Global column via `isGlobalWindow`).
- `Cursor Models` uses `autoPercentUsed`, never `totalPercentUsed`.
- `plan.limit` / `plan.used` / `plan.remaining` from the API must never be rendered as a budget — they are not the denominator of any of the three percentages.
- `CURSOR_INCLUDED_USD` is removed entirely; no fallback to the old plan-table computation.
- No new third-party dependencies.
- Network access lives only in `fetchCursorReports`. Every mapping function stays pure and unit-tested without a server.
- Commit messages follow the repo's Conventional Commits style and carry **no** `Co-Authored-By` trailer.
- `docs/superpowers` is ignored by the user's global gitignore but tracked in this repo — always `git add -f` files under that path.

---

### Task 1: Wire types and the pure summary → reports mapper

Adds the new response types and the pure mapping function, with a real (redacted) fixture. Nothing calls it yet; the old Cursor code path stays untouched and green.

**Files:**
- Modify: `internal/limits/cursor.go` (append; do not delete anything yet)
- Create: `internal/limits/testdata/cursor_usage_summary.json`
- Test: `internal/limits/cursor_test.go` (append)

**Interfaces:**
- Consumes: `Report`, `Window` (`internal/limits/format.go:13-26`); `errAgentUnavailable` (`internal/limits/run.go:58`); `cursorPlanName(membership string) string` (`internal/limits/cursor.go:195`); `isGlobalWindow(w Window) bool` (`internal/limits/format.go:396`).
- Produces:
  - `type cursorUsageSummary struct` with fields `BillingCycleStart, BillingCycleEnd, MembershipType string`, `IsUnlimited bool`, `IndividualUsage, TeamUsage cursorUsageBlock`
  - `type cursorUsageBlock struct { Plan *cursorPlanUsage }`
  - `type cursorPlanUsage struct { Enabled bool; AutoPercentUsed, APIPercentUsed float64 }`
  - `func cursorPlanBlock(s *cursorUsageSummary) *cursorPlanUsage`
  - `func cursorSummaryToReports(s *cursorUsageSummary) ([]Report, error)`

- [ ] **Step 1: Create the fixture**

Create `internal/limits/testdata/cursor_usage_summary.json`. This is a real Ultra-account response with the display messages kept verbatim, since they document why `Cursor Models` diverges from Cursor's own UI:

```json
{
  "billingCycleStart": "2026-07-25T13:40:53.000Z",
  "billingCycleEnd": "2026-08-25T13:40:53.000Z",
  "membershipType": "ultra",
  "limitType": "user",
  "isUnlimited": false,
  "autoModelSelectedDisplayMessage": "You've used 12% of your included total usage",
  "namedModelSelectedDisplayMessage": "You've used 42% of your included API usage",
  "individualUsage": {
    "plan": {
      "enabled": true,
      "used": 31210,
      "limit": 40000,
      "remaining": 8790,
      "breakdown": { "included": 31210, "bonus": 0, "total": 31210 },
      "autoPercentUsed": 5.219,
      "apiPercentUsed": 41.544,
      "totalPercentUsed": 12.484
    },
    "onDemand": { "enabled": true, "used": 0, "limit": 50000, "remaining": 50000 }
  },
  "teamUsage": {}
}
```

- [ ] **Step 2: Write the failing tests**

Append to `internal/limits/cursor_test.go`. Note the new imports needed at the top of the file: `encoding/json`, `os`, `path/filepath` (the file already imports `encoding/base64`, `errors`, `testing`, `time`).

```go
// loadCursorSummary decodes a testdata fixture into the wire type, so the
// mapping tests exercise the same JSON path production does.
func loadCursorSummary(t *testing.T, name string) *cursorUsageSummary {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var s cursorUsageSummary
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	return &s
}

func TestCursorSummaryToReports_Individual(t *testing.T) {
	reports, err := cursorSummaryToReports(loadCursorSummary(t, "cursor_usage_summary.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reports) != 2 {
		t.Fatalf("got %d reports, want exactly 2", len(reports))
	}

	wantEnd := time.Date(2026, 8, 25, 13, 40, 53, 0, time.UTC)
	cases := []struct {
		provider string
		pct      float64
	}{
		{"Cursor Models", 5.219},
		{"Cursor API", 41.544},
	}
	for i, c := range cases {
		r := reports[i]
		if r.Provider != c.provider {
			t.Errorf("reports[%d].Provider = %q, want %q", i, r.Provider, c.provider)
		}
		if len(r.Windows) != 1 {
			t.Fatalf("reports[%d]: got %d windows, want exactly 1", i, len(r.Windows))
		}
		w := r.Windows[0]
		if w.UsedPercent != c.pct {
			t.Errorf("%s UsedPercent = %v, want %v", c.provider, w.UsedPercent, c.pct)
		}
		if !w.ResetsAt.Equal(wantEnd) {
			t.Errorf("%s ResetsAt = %v, want %v", c.provider, w.ResetsAt, wantEnd)
		}
		if w.WindowMinutes != 31*24*60 {
			t.Errorf("%s WindowMinutes = %d, want %d", c.provider, w.WindowMinutes, 31*24*60)
		}
		if !isGlobalWindow(w) {
			t.Errorf("%s: monthly window should classify as global, got label %q", c.provider, w.Label)
		}
		if r.Note == "" {
			t.Errorf("%s: expected a non-empty disclaimer Note", c.provider)
		}
		if !strings.Contains(r.Source, "Ultra") {
			t.Errorf("%s Source = %q, want it to name the plan", c.provider, r.Source)
		}
	}
}

// The Models row must never borrow totalPercentUsed (12.484 in the fixture):
// that would double-count the API spend the second row already reports.
func TestCursorSummaryToReports_ModelsRowUsesAutoNotTotal(t *testing.T) {
	reports, err := cursorSummaryToReports(loadCursorSummary(t, "cursor_usage_summary.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := reports[0].Windows[0].UsedPercent; got == 12.484 {
		t.Fatalf("Models row used totalPercentUsed (%v); it must use autoPercentUsed", got)
	}
}

func TestCursorSummaryToReports_Unlimited(t *testing.T) {
	s := loadCursorSummary(t, "cursor_usage_summary.json")
	s.IsUnlimited = true
	if _, err := cursorSummaryToReports(s); !errors.Is(err, errAgentUnavailable) {
		t.Fatalf("err = %v, want errAgentUnavailable", err)
	}
}

func TestCursorSummaryToReports_PlanDisabled(t *testing.T) {
	s := loadCursorSummary(t, "cursor_usage_summary.json")
	s.IndividualUsage.Plan.Enabled = false
	if _, err := cursorSummaryToReports(s); !errors.Is(err, errAgentUnavailable) {
		t.Fatalf("err = %v, want errAgentUnavailable", err)
	}
}

// Team accounts report through teamUsage with the same shape; the individual
// block is absent.
func TestCursorSummaryToReports_FallsBackToTeamUsage(t *testing.T) {
	s := loadCursorSummary(t, "cursor_usage_summary.json")
	s.TeamUsage.Plan = s.IndividualUsage.Plan
	s.IndividualUsage.Plan = nil
	reports, err := cursorSummaryToReports(s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reports) != 2 {
		t.Fatalf("got %d reports, want 2", len(reports))
	}
	if got := reports[1].Windows[0].UsedPercent; got != 41.544 {
		t.Errorf("API UsedPercent = %v, want 41.544", got)
	}
}

func TestCursorSummaryToReports_NoUsageBlocks(t *testing.T) {
	s := loadCursorSummary(t, "cursor_usage_summary.json")
	s.IndividualUsage.Plan = nil
	if _, err := cursorSummaryToReports(s); !errors.Is(err, errAgentUnavailable) {
		t.Fatalf("err = %v, want errAgentUnavailable", err)
	}
}

func TestCursorSummaryToReports_MalformedCycleDates(t *testing.T) {
	t.Run("start", func(t *testing.T) {
		s := loadCursorSummary(t, "cursor_usage_summary.json")
		s.BillingCycleStart = "not-a-date"
		_, err := cursorSummaryToReports(s)
		if err == nil || !strings.Contains(err.Error(), "billingCycleStart") {
			t.Fatalf("err = %v, want an error naming billingCycleStart", err)
		}
	})
	t.Run("end", func(t *testing.T) {
		s := loadCursorSummary(t, "cursor_usage_summary.json")
		s.BillingCycleEnd = ""
		_, err := cursorSummaryToReports(s)
		if err == nil || !strings.Contains(err.Error(), "billingCycleEnd") {
			t.Fatalf("err = %v, want an error naming billingCycleEnd", err)
		}
	})
}

func TestCursorPlanName(t *testing.T) {
	cases := map[string]string{
		"ultra":    "Ultra",
		"pro":      "Pro",
		"pro_plus": "Pro+",
		"business": "Business",
		"":         "Cursor",
	}
	for in, want := range cases {
		if got := cursorPlanName(in); got != want {
			t.Errorf("cursorPlanName(%q) = %q, want %q", in, got, want)
		}
	}
}
```

Add `"strings"` to the test file's import block as well.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/limits/ -run 'TestCursorSummaryToReports|TestCursorPlanName' -v`
Expected: compile failure — `undefined: cursorUsageSummary`, `undefined: cursorSummaryToReports`.

- [ ] **Step 4: Add the wire types**

Append to `internal/limits/cursor.go`:

```go
// cursorUsageSummary is the (subset of the) shape returned by
// GET /api/usage-summary on cursor.com — the endpoint Cursor's dashboard uses
// to render its usage headline. Fields we don't consume are omitted.
//
// Cursor meters two disjoint pools against two separate allowances and reports
// each as its own percentage: autoPercentUsed for the Auto/Composer pool and
// apiPercentUsed for the usage-based (API) pool. Note that plan.used/limit are
// NOT the denominators of those percentages, which is why they are not parsed.
type cursorUsageSummary struct {
	BillingCycleStart string           `json:"billingCycleStart"`
	BillingCycleEnd   string           `json:"billingCycleEnd"`
	MembershipType    string           `json:"membershipType"`
	IsUnlimited       bool             `json:"isUnlimited"`
	IndividualUsage   cursorUsageBlock `json:"individualUsage"`
	TeamUsage         cursorUsageBlock `json:"teamUsage"`
}

// cursorUsageBlock is one of the two parallel usage scopes in the response.
// Individual accounts populate individualUsage and leave teamUsage as {};
// team-billed accounts do the reverse.
type cursorUsageBlock struct {
	Plan *cursorPlanUsage `json:"plan"`
}

type cursorPlanUsage struct {
	Enabled         bool    `json:"enabled"`
	AutoPercentUsed float64 `json:"autoPercentUsed"`
	APIPercentUsed  float64 `json:"apiPercentUsed"`
}
```

- [ ] **Step 5: Add the mapper**

Append to `internal/limits/cursor.go`:

```go
// cursorPlanBlock picks the usage scope that applies to this account: the
// individual plan when enabled, otherwise the team plan. Returns nil when
// neither is usable, which the caller turns into errAgentUnavailable.
func cursorPlanBlock(s *cursorUsageSummary) *cursorPlanUsage {
	if p := s.IndividualUsage.Plan; p != nil && p.Enabled {
		return p
	}
	if p := s.TeamUsage.Plan; p != nil && p.Enabled {
		return p
	}
	return nil
}

// cursorSummaryToReports maps one usage-summary response to the two rows
// lazyagent shows for Cursor. Kept pure so the mapping is unit-testable without
// a server; all network work lives in fetchCursorReports.
func cursorSummaryToReports(s *cursorUsageSummary) ([]Report, error) {
	if s.IsUnlimited {
		return nil, fmt.Errorf("%w: Cursor reports this account as unlimited, so there is no budget to pace against", errAgentUnavailable)
	}
	plan := cursorPlanBlock(s)
	if plan == nil {
		return nil, fmt.Errorf("%w: Cursor is signed in but reports no enabled usage plan for this account", errAgentUnavailable)
	}
	start, err := time.Parse(time.RFC3339, s.BillingCycleStart)
	if err != nil {
		return nil, fmt.Errorf("parse Cursor billingCycleStart %q: %w", s.BillingCycleStart, err)
	}
	end, err := time.Parse(time.RFC3339, s.BillingCycleEnd)
	if err != nil {
		return nil, fmt.Errorf("parse Cursor billingCycleEnd %q: %w", s.BillingCycleEnd, err)
	}

	windowMinutes := int(end.Sub(start).Minutes())
	planName := cursorPlanName(s.MembershipType)
	cycle := fmt.Sprintf("billing cycle %s – %s", start.Local().Format("2 Jan"), end.Local().Format("2 Jan"))
	window := func(pct float64) []Window {
		return []Window{{
			Label:         "monthly",
			WindowMinutes: windowMinutes,
			UsedPercent:   pct,
			ResetsAt:      end,
		}}
	}

	return []Report{
		{
			Provider: "Cursor Models",
			Source: fmt.Sprintf("Source: %.1f%% of the plan's included Auto/Composer allowance (%s); %s",
				plan.AutoPercentUsed, planName, cycle),
			Windows: window(plan.AutoPercentUsed),
			Note: "Note: reads /api/usage-summary on cursor.com with the Cursor session token from state.vscdb — the Auto/Composer pool's own allowance (autoPercentUsed). Cursor's dashboard shows the combined total instead when Auto is the selected model, so this figure reads lower than the one in its UI. Undocumented; may break or be revoked by Cursor without notice.",
		},
		{
			Provider: "Cursor API",
			Source: fmt.Sprintf("Source: %.1f%% of the plan's included API allowance (%s); %s",
				plan.APIPercentUsed, planName, cycle),
			Windows: window(plan.APIPercentUsed),
			Note:    "Note: reads /api/usage-summary on cursor.com with the Cursor session token from state.vscdb — the usage-based (API) pool only. Undocumented; may break or be revoked by Cursor without notice.",
		},
	}, nil
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/limits/ -run 'TestCursorSummaryToReports|TestCursorPlanName' -v`
Expected: PASS, 8 tests (including subtests).

- [ ] **Step 7: Run the full package suite**

Run: `go test ./internal/limits/`
Expected: PASS — the old Cursor code and its tests are untouched and still green.

- [ ] **Step 8: Commit**

```bash
git add internal/limits/cursor.go internal/limits/cursor_test.go internal/limits/testdata/cursor_usage_summary.json
git commit -m "feat(limits): map Cursor's usage-summary response to two reports"
```

---

### Task 2: Dispatcher returns a slice of reports per agent

Pure refactor, no behavior change: every agent still produces exactly one report. This isolates the signature change so it can be reviewed on its own, and unblocks Cursor emitting two rows in Task 3.

**Files:**
- Modify: `internal/limits/run.go:230-245` (`fetchReport`), `internal/limits/run.go:155-184` (the Run loop)
- Modify: `internal/limits/view.go:69-95` (`FetchAll`)
- Create: `internal/limits/run_test.go`

**Interfaces:**
- Consumes: `fetchClaudeReport`, `fetchCodexReport`, `fetchGrokReport`, `fetchKimiReport`, `fetchCursorReport` — all still `func(context.Context) (Report, error)` at this point.
- Produces:
  - `func fetchReports(ctx context.Context, agent string) ([]Report, error)` — replaces `fetchReport`
  - `func single(r Report, err error) ([]Report, error)` — adapter wrapping a one-report fetcher

- [ ] **Step 1: Write the failing tests**

Create `internal/limits/run_test.go`:

```go
package limits

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSingleWrapsOneReport(t *testing.T) {
	got, err := single(Report{Provider: "Grok"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Provider != "Grok" {
		t.Fatalf("got %+v, want a one-element slice holding the Grok report", got)
	}
}

func TestSinglePropagatesErrorAndDropsTheReport(t *testing.T) {
	sentinel := errors.New("boom")
	got, err := single(Report{Provider: "Grok"}, sentinel)
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the sentinel", err)
	}
	if got != nil {
		t.Fatalf("got %+v, want nil reports alongside an error", got)
	}
}

func TestFetchReportsRejectsUnknownAgent(t *testing.T) {
	_, err := fetchReports(context.Background(), "nope")
	if err == nil || !strings.Contains(err.Error(), "unsupported agent") {
		t.Fatalf("err = %v, want an unsupported-agent error", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/limits/ -run 'TestSingle|TestFetchReports' -v`
Expected: compile failure — `undefined: single`, `undefined: fetchReports`.

- [ ] **Step 3: Replace `fetchReport` with `fetchReports`**

In `internal/limits/run.go`, replace the whole `fetchReport` function (lines 230-245) with:

```go
// fetchReports dispatches to one agent's fetcher. It returns a slice because a
// single agent can meter more than one independent pool — Cursor reports its
// Auto/Composer and usage-based API allowances as two separate rows.
func fetchReports(ctx context.Context, agent string) ([]Report, error) {
	switch agent {
	case "claude":
		return single(fetchClaudeReport(ctx))
	case "codex":
		return single(fetchCodexReport(ctx))
	case "grok":
		return single(fetchGrokReport(ctx))
	case "kimi":
		return single(fetchKimiReport(ctx))
	case "cursor":
		return single(fetchCursorReport(ctx))
	default:
		return nil, fmt.Errorf("unsupported agent %q", agent)
	}
}

// single adapts a one-report fetcher to the dispatcher's slice signature.
func single(r Report, err error) ([]Report, error) {
	if err != nil {
		return nil, err
	}
	return []Report{r}, nil
}
```

- [ ] **Step 4: Update the Run loop**

In `internal/limits/run.go`, in the `for _, a := range agents` loop, change the first line from `report, err := fetchReport(ctx, a)` to:

```go
		rs, err := fetchReports(ctx, a)
```

and change the final line of the loop body from `reports = append(reports, report)` to:

```go
		reports = append(reports, rs...)
```

Leave every `errors.Is` branch in between exactly as it is — not-installed and unavailable still apply to the agent as a whole.

- [ ] **Step 5: Update `FetchAll`**

In `internal/limits/view.go`, replace the body of `FetchAll` (lines 69-95) with:

```go
func FetchAll(ctx context.Context) []Report {
	agents, _ := resolveAgents("all") // "all" is always a valid argument, so the error is never non-nil here.
	results := make([][]Report, len(agents))
	var wg sync.WaitGroup
	for i, a := range agents {
		wg.Add(1)
		go func(i int, a string) {
			defer wg.Done()
			rs, err := fetchReports(ctx, a)
			if err != nil {
				return
			}
			results[i] = rs
		}(i, a)
	}
	wg.Wait()

	var out []Report
	for i := range agents {
		out = append(out, results[i]...)
	}
	return out
}
```

The parallel `found []bool` slice is deleted: a failed agent leaves its slot nil, and appending a nil slice contributes nothing.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/limits/ -run 'TestSingle|TestFetchReports' -v`
Expected: PASS, 3 tests.

- [ ] **Step 7: Verify nothing else regressed**

Run: `go build ./... && go vet ./internal/... && go test ./...`
Expected: build clean, vet silent, all tests PASS. No output should have changed — this task is behavior-preserving.

- [ ] **Step 8: Commit**

```bash
git add internal/limits/run.go internal/limits/view.go internal/limits/run_test.go
git commit -m "refactor(limits): let one agent yield multiple reports"
```

---

### Task 3: Point Cursor at `/api/usage-summary` and delete the old path

Cursor now emits two rows. The plan table, the env-var override, the cycle arithmetic and the aggregation split all go.

**Files:**
- Modify: `internal/limits/cursor.go` (delete the old fetcher and its helpers, add the new fetcher)
- Modify: `internal/limits/run.go` (dispatcher case for `cursor`)
- Modify: `internal/limits/cursor_test.go` (delete tests for deleted functions)
- Modify: `internal/ui/limits_test.go:44` (provider list)

**Interfaces:**
- Consumes: `cursorSummaryToReports` and `cursorUsageSummary` (Task 1); `fetchReports` (Task 2); `cursor.ReadAuth()` (`internal/cursor/auth.go:16`); `cursorUserIDFromToken`, `cursorGET`, `cursorDo`.
- Produces: `func fetchCursorReports(ctx context.Context) ([]Report, error)` — replaces `fetchCursorReport`.

- [ ] **Step 1: Establish the guard before deleting**

This task's deliverable — the fetcher — cannot be unit-tested without a signed-in Cursor, so the safety net is Task 1's mapping suite plus the live run in Step 8. Confirm the net is in place before removing anything.

Run: `go test ./internal/limits/ -run TestCursorSummaryToReports -v`
Expected: PASS, covering row order, both percentages, window bounds, the team fallback, and every unavailable case. These must stay green through every step below; if one goes red after a deletion, the deletion was wrong.

- [ ] **Step 2: Delete the old Cursor implementation**

From `internal/limits/cursor.go`, delete these declarations entirely. Line numbers refer to the file as it stood before Task 1, which only appended — so they are still accurate: the `cursorAutoTier` const and its leading comment block (lines 18-27), `cursorIncludedByPlan` (29-39), `cursorUsageResponse` (41-43), `cursorAggResponse` (45-48), `cursorAgg` (50-54), `cursorUsage` (56-65), `fetchCursorReport` (67-119), `cursorSpendByPool` (148-159), `cursorIncludedUSD` (161-166), `cursorIncluded` (168-193), `cursorBillingCycle` (208-220), `cursorUsageToReport` (222-240), and `cursorPOST` (250-261).

Keep: `cursorUserIDFromToken`, `cursorPlanName`, `cursorGET`, `cursorDo`, and everything added in Task 1.

Then fix the import block — `os` and `strconv` are now unused:

```go
import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/illegalstudio/lazyagent/internal/cursor"
)
```

- [ ] **Step 3: Add the new fetcher**

Add to `internal/limits/cursor.go`, above `cursorUserIDFromToken`:

```go
// cursorUsageSummaryURL is the (undocumented) endpoint Cursor's own dashboard
// calls to render its usage headline. See the package doc in run.go for the
// stability caveat.
const cursorUsageSummaryURL = "https://cursor.com/api/usage-summary"

// fetchCursorReports returns Cursor's two metered pools as separate reports:
// the Auto/Composer allowance and the usage-based API allowance. Both come from
// one call, so they always appear or disappear together.
func fetchCursorReports(ctx context.Context) ([]Report, error) {
	token, _, ok, err := cursor.ReadAuth()
	if err != nil {
		return nil, fmt.Errorf("read Cursor credentials: %w", err)
	}
	if !ok {
		return nil, errAgentNotInstalled
	}

	userID, err := cursorUserIDFromToken(token)
	if err != nil {
		return nil, fmt.Errorf("decode Cursor access token: %w", err)
	}
	cookie := userID + "%3A%3A" + token

	var summary cursorUsageSummary
	if err := cursorGET(ctx, cursorUsageSummaryURL, cookie, &summary); err != nil {
		return nil, err
	}
	return cursorSummaryToReports(&summary)
}
```

The membership type from `state.vscdb` is deliberately discarded — the response carries an authoritative `membershipType`.

- [ ] **Step 4: Rewire the dispatcher**

In `internal/limits/run.go`, in `fetchReports`, change the Cursor case from `return single(fetchCursorReport(ctx))` to:

```go
	case "cursor":
		return fetchCursorReports(ctx)
```

- [ ] **Step 5: Delete the tests for the deleted functions**

From `internal/limits/cursor_test.go`, delete `TestCursorSpendByPool`, `TestCursorIncludedUSD`, `TestCursorIncluded`, `TestCursorBillingCycle`, and `TestCursorUsageToReport`. Keep `TestCursorUserIDFromToken`, `makeJWT`, and everything added in Task 1.

- [ ] **Step 6: Update the UI test's provider list**

In `internal/ui/limits_test.go:44`, change:

```go
	for _, p := range []string{"Claude Code", "Codex", "Grok", "Kimi Code", "Cursor"} {
```

to:

```go
	for _, p := range []string{"Claude Code", "Codex", "Grok", "Kimi Code", "Cursor Models", "Cursor API"} {
```

This widens the modal-overflow guard to the six rows the limits view can now hold.

- [ ] **Step 7: Verify the whole tree**

Run: `go build ./... && go vet ./internal/... && go test ./...`
Expected: build clean, vet silent, all tests PASS. If vet reports an unused import or an undefined symbol in `cursor.go`, a deletion in Step 2 was incomplete.

- [ ] **Step 8: Verify against the live account**

Run: `go run . limits --agent cursor --detailed`
Expected: two blocks, `Cursor Models` then `Cursor API`, each with a `monthly` window, a reset time at the end of the current billing cycle, and a `Source:` line naming the plan. The Models percentage should be materially lower than the API one. Compare both against cursor.com's dashboard — the API figure must match the "% of your included API usage" headline.

Then run `go run . limits` and confirm both rows appear in the summary table with `--` in the `5h` column.

If Cursor is not signed in on this machine, the expected output is instead `Cursor is not installed or not logged in (no token in state.vscdb). Open Cursor and sign in.` on stderr with exit code 1 — record which of the two you saw.

- [ ] **Step 9: Commit**

```bash
git add internal/limits/cursor.go internal/limits/cursor_test.go internal/limits/run.go internal/ui/limits_test.go
git commit -m "feat(limits): split Cursor into Models and API rows via /api/usage-summary"
```

---

### Task 4: Documentation

Every user-facing mention of the old single Cursor row, the plan table, and `CURSOR_INCLUDED_USD` is updated. No code changes.

**Files:**
- Modify: `internal/limits/run.go` (package doc, `--help` text)
- Modify: `docs/maintenance/limits.md`
- Modify: `docs/usage/cli.md:259`
- Modify: `docs/reference/roadmap.md:175`
- Modify: `README.md:40`

**Interfaces:**
- Consumes: the behavior implemented in Tasks 1-3. Nothing produces new symbols.

- [ ] **Step 1: Update the package doc comment**

In `internal/limits/run.go`, replace the `IMPORTANT (Cursor)` paragraph (lines 28-32) with:

```go
// IMPORTANT (Cursor): the source for Cursor is /api/usage-summary on cursor.com —
// the same endpoint the Cursor dashboard uses to render its usage headline. It is
// read with the session token from Cursor's local state.vscdb. Cursor meters two
// disjoint pools against separate allowances, so it is reported as two rows:
// "Cursor Models" (the Auto/Composer pool, autoPercentUsed) and "Cursor API" (the
// usage-based pool, apiPercentUsed). Same caveats as the others: on-demand only,
// undocumented, fail gracefully.
```

The package doc's opening paragraph (lines 1-4) needs no change — it lists Cursor among the providers without describing its window shape.

- [ ] **Step 2: Update the `--help` text**

In `internal/limits/run.go`, in the `fs.Usage` string:

- Change `lazyagent limits --agent cursor   Show only Cursor limits (API usage pool)` to `lazyagent limits --agent cursor   Show only Cursor limits (Models + API pools)`.
- Replace the `Cursor` block under `Authentication:` (lines 117-120) with:

```
  Cursor  reads its session token from Cursor's local state.vscdb.
          If none is found, open Cursor and sign in. Cursor reports its
          Auto/Composer and usage-based API pools as separate percentages,
          shown as two rows.
```

- [ ] **Step 3: Rewrite the Cursor section of `docs/maintenance/limits.md`**

Replace item 2 of the numbered list and the two paragraphs that follow it (the `With that cookie it makes two HTTPS calls…`, `**Two pools, one window.**` and `**The included budget.**` paragraphs) with:

```markdown
2. **`cursorAuth/stripeMembershipType`** — the plan (`pro`, `pro_plus`, `ultra`, …). Read for Cursor session monitoring; the limits report takes the plan name from the API response instead.

With that cookie it makes one HTTPS call to `cursor.com`: `GET /api/usage-summary`, the same endpoint the dashboard uses for its usage headline.

**Two pools, two rows.** Cursor meters spend into an **Auto / Composer** pool and a **usage-based API** pool, each against its own allowance, and reports each as its own percentage. lazyagent shows both: `Cursor Models` uses `autoPercentUsed`, `Cursor API` uses `apiPercentUsed`. They share one monthly window bounded by `billingCycleStart` and `billingCycleEnd`, and both appear or disappear together since one call produces both.

**Why `Cursor Models` reads lower than Cursor's UI.** When Auto is the selected model, Cursor's own dashboard shows the *combined* figure (`totalPercentUsed`), not the Auto pool's own utilization. lazyagent deliberately shows `autoPercentUsed`: with two separate rows, using the combined total would double-count the API spend the second row already reports.

**No dollar amounts.** The endpoint returns percentages, not the per-pool split in currency. It does carry `plan.used` / `plan.limit`, but those are not the denominators of any of the three percentages, so rendering them would mislead. The percentage comes straight from Cursor — there is no plan table to keep in sync and nothing to override.
```

Then, earlier in the same file, replace the clause `Cursor exposes a single **monthly** window tracking its usage-based (API) spend against the plan's included credit` with:

```markdown
Cursor exposes two **monthly** rows sharing one billing-cycle window — its Auto/Composer pool and its usage-based API pool, each against its own allowance
```

And in the sample summary table under "## Output", replace the single `Cursor` row with two:

```
│ Cursor Models │ --                     │  5.2% used / 54.8% exp │
│ Cursor API    │ --                     │ 41.5% used / 54.8% exp │
```

Widen the table's box-drawing rules to match the longest label (`Cursor Models`), and update the sentence after the table that reads `Grok and Cursor use their monthly billing window. Cursor never populates the `5h` column` so it refers to both Cursor rows.

- [ ] **Step 4: Update the skip-case and disclaimer sections of `docs/maintenance/limits.md`**

Replace the `**Cursor signed in, but on an unmapped plan.**` paragraph with:

```markdown
**Cursor signed in, but with no budget to pace.** There's a second skip case unique to Cursor: you're signed in, but Cursor reports the account as unlimited (`isUnlimited`) or returns no enabled usage plan. Both rows are skipped together — silently under `--agent all`, with an actionable message under `--agent cursor`. Plans outside the consumer tiers (Business, Enterprise, Teams) no longer need any configuration: Cursor computes the percentage for them too.
```

In the disclaimers section, replace the sentence:

```markdown
For **Cursor**, `/api/usage` and `/api/dashboard/get-aggregated-usage-events` on `cursor.com` are not documented as a public API. As of this writing they are used internally by the Cursor dashboard's usage summary and are subject to:
```

with:

```markdown
For **Cursor**, `/api/usage-summary` on `cursor.com` is not documented as a public API. As of this writing it is used internally by the Cursor dashboard's usage headline and is subject to:
```

Replace the `**No stability guarantee**` bullet, which cites a `tier` field that no longer factors in:

```markdown
- **No stability guarantee** — the endpoint path, the response shape, and the `autoPercentUsed` / `apiPercentUsed` fields may change without notice. lazyagent fails gracefully when this happens.
```

Replace the `**Plan-derived budget**` bullet with:

```markdown
- **Vendor-computed percentages** — the figures come from Cursor's own `autoPercentUsed` / `apiPercentUsed`, so they track whatever allowances Cursor applies to your plan. If Cursor renames or restructures those fields, lazyagent fails gracefully rather than reporting a stale number.
```

- [ ] **Step 5: Remove `CURSOR_INCLUDED_USD` from both env tables**

Delete the `CURSOR_INCLUDED_USD` row from the Environment table at the bottom of `docs/maintenance/limits.md`, and the matching row in `docs/usage/cli.md:259`.

- [ ] **Step 6: Update the roadmap and README**

In `docs/reference/roadmap.md:175`, replace the Cursor bullet with:

```markdown
- ✅ Cursor via `/api/usage-summary` on `cursor.com` (the same endpoint its dashboard uses), session token read from local `state.vscdb`; reports the Auto/Composer and usage-based API pools as two rows against Cursor's own per-pool percentages
```

In `README.md:40`, change `and Cursor (monthly API usage)` to `and Cursor (monthly, Models + API pools)`.

- [ ] **Step 7: Verify no stale references remain**

Run: `grep -rn "CURSOR_INCLUDED_USD\|get-aggregated-usage-events\|cursorIncludedByPlan" --include="*.go" --include="*.md" . | grep -v node_modules | grep -v docs/superpowers`
Expected: no output. Hits under `docs/superpowers/` are expected and correct — the spec records the old behavior on purpose.

Run: `go test ./... && go run . limits --help`
Expected: tests PASS; the help text names both Cursor pools and no longer mentions `CURSOR_INCLUDED_USD`.

- [ ] **Step 8: Commit**

```bash
git add internal/limits/run.go docs/maintenance/limits.md docs/usage/cli.md docs/reference/roadmap.md README.md
git commit -m "docs(limits): document Cursor's Models and API rows"
```

---

## Verification checklist

Run after Task 4, before opening a PR:

- [ ] `go build ./...` — clean
- [ ] `go vet ./internal/...` — silent
- [ ] `go test ./...` — all PASS
- [ ] `go run . limits` — `Cursor Models` and `Cursor API` both present, `--` in the `5h` column
- [ ] `go run . limits --detailed` — both blocks carry a `Source:`, a `Note:`, and a reset time at the cycle end
- [ ] The `Cursor API` percentage matches cursor.com's "% of your included API usage" headline
- [ ] TUI: press `l`, confirm both rows in Summary and both blocks in Detailed
- [ ] `grep -rn "CURSOR_INCLUDED_USD" --include="*.go" --include="*.md" . | grep -v docs/superpowers` — no output

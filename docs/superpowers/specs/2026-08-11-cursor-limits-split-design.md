# Cursor limits — `Cursor Models` and `Cursor API` rows — Design

**Date:** 2026-08-11
**Branch:** `feat/cursor-limits-split`

## Goal

`lazyagent limits` currently reports Cursor as a single row covering only the
usage-based (API) pool. Cursor meters two disjoint pools against two distinct
budgets, and exposes both. Report them as two rows:

```
│ Cursor Models │ -- │  5.2% used / 54.8% exp │
│ Cursor API    │ -- │ 41.5% used / 54.8% exp │
```

## Background: what Cursor actually exposes

`GET https://cursor.com/api/usage-summary` — undocumented, authenticated with
the same `WorkosCursorSessionToken` cookie the current implementation already
builds — returns:

```json
{
  "billingCycleStart": "2026-07-25T13:40:53.000Z",
  "billingCycleEnd":   "2026-08-25T13:40:53.000Z",
  "membershipType": "ultra",
  "limitType": "user",
  "isUnlimited": false,
  "autoModelSelectedDisplayMessage":  "You've used 12% of your included total usage",
  "namedModelSelectedDisplayMessage": "You've used 42% of your included API usage",
  "individualUsage": {
    "plan":     { "enabled": true, "used": 31210, "limit": 40000, "remaining": 8790,
                  "breakdown": { "included": 31210, "bonus": 0, "total": 31210 },
                  "autoPercentUsed": 5.219, "apiPercentUsed": 41.544,
                  "totalPercentUsed": 12.484 },
    "onDemand": { "enabled": true, "used": 0, "limit": 50000, "remaining": 50000 }
  },
  "teamUsage": {}
}
```

Units are cents: `onDemand.limit` 50000 equals the `hardLimit` 500 returned by
`/api/dashboard/get-hard-limit`.

### The three percentages are per-pool, on disjoint denominators

The three percentages resolve to a single consistent solution, verified against
an Ultra account:

| Pool | Spend in cycle | Denominator | Percent |
|------|---------------:|------------:|--------:|
| API (`tier != 2`)             | $207.72 | $500  | 41.544% |
| Auto/Composer (`tier == 2`)   | $104.38 | $2000 |  5.219% |
| Total                         | $312.10 | $2500 | 12.484% |

Cross-checked independently: summing `totalCents` from
`/api/dashboard/get-aggregated-usage-events` over the exact billing cycle gives
API $207.23 and Auto $104.38. The 0.24% delta on the API pool is usage recorded
between the two calls.

### Consequences

1. **The Auto pool has a real budget.** The comment at `cursor.go:18-21`
   ("effectively unlimited on paid plans … does NOT draw from the included
   credit") is superseded. `autoPercentUsed` is a real, metered figure.
2. **The current percentage is wrong.** Today lazyagent computes
   `apiSpend / cursorIncludedByPlan[plan]` = `207.72 / 400` = 51.9%, while
   Cursor's own denominator is $500, giving 41.5%. The plan table is not the
   denominator Cursor uses.
3. **`plan.limit` is not the denominator of any of the three percentages.**
   `31210 / 40000` = 78%, not 12.484%. It must not be rendered as a budget.
4. **The billing cycle no longer needs to be derived.** `billingCycleEnd` is
   returned directly, replacing `cursorBillingCycle()`'s calendar-month
   arithmetic and its short-month clamping.

## Decisions (confirmed)

1. **Single source: `/api/usage-summary`.** One GET replaces the current two
   calls (`/api/usage` + `/api/dashboard/get-aggregated-usage-events`).
   Consequence accepted: `Source` lines carry percentages, not dollar amounts,
   because the endpoint does not expose the per-pool split in currency.
2. **`CURSOR_INCLUDED_USD` is removed**, along with the per-plan table. Cursor
   computes the percentage for every plan, Business/Enterprise/Teams included,
   so there is no denominator left to override. Documented breaking change; an
   exported value is silently ignored.
3. **`Cursor Models` uses `autoPercentUsed`, not `totalPercentUsed`** — see
   "Which percentage for the Models row" below.
4. **Two reports from one fetch**, not two pseudo-agents.
5. **Row order: `Cursor Models`, then `Cursor API`.**

## Data source

`fetchCursorReports` performs one `cursorGET` to
`https://cursor.com/api/usage-summary`, reusing `cursorUserIDFromToken`,
`cursorGET` and `cursorDo` unchanged.

Fields consumed: `billingCycleStart`, `billingCycleEnd`, `membershipType`,
`isUnlimited`, and from the usage block `plan.enabled`, `plan.autoPercentUsed`,
`plan.apiPercentUsed`.

**Usage block selection.** Prefer `individualUsage` when its `plan` block is
present and enabled; otherwise fall back to `teamUsage`, which has the same
shape. Team accounts were not available for verification, so the fallback is
written defensively: if neither block yields an enabled `plan`, Cursor is
skipped rather than reported as 0%.

### Code removed

`/api/usage` and `/api/dashboard/get-aggregated-usage-events` calls,
`cursorUsageResponse`, `cursorAggResponse`, `cursorAgg`, `cursorUsage`,
`cursorSpendByPool`, `cursorAutoTier`, `cursorIncludedByPlan`,
`cursorIncludedUSD`, `cursorIncluded`, `cursorPlanName`, `cursorBillingCycle`,
`cursorUsageToReport`, and `cursorPOST` (left without callers).

Retained: `cursorUserIDFromToken`, `cursorGET`, `cursorDo`.

## Two reports from one fetch

`fetchReport(ctx, agent) (Report, error)` becomes
`fetchReports(ctx, agent) ([]Report, error)`. Claude, Codex, Grok and Kimi wrap
their single report in a one-element slice; Cursor returns two.

- `run.go`: `reports = append(reports, rs...)`. Error handling is unchanged —
  `errAgentNotInstalled` and `errAgentUnavailable` still apply to the agent as a
  whole, so Cursor's two rows always appear or disappear together.
- `view.go`: `FetchAll`'s per-agent result slot becomes `[]Report`, flattened in
  the existing canonical agent order.

Rejected alternative: registering `cursor-models` and `cursor-api` as separate
entries in `resolveAgents`. It would either double the HTTP calls or require a
per-run cache, and would expand the `--agent` flag surface. With the chosen
design `--agent cursor` still makes one call and prints both rows.

`Report.Provider` values are `"Cursor Models"` and `"Cursor API"`.
`summaryProviderName` passes them through unchanged. The TUI and GUI consume
`limits.View` generically and need no changes; only the hardcoded provider list
in `internal/ui/limits_test.go:44` is updated.

## Windows

Both reports carry a single window:

| Field | Value |
|-------|-------|
| `Label` | `"monthly"` |
| `WindowMinutes` | `billingCycleEnd - billingCycleStart` |
| `ResetsAt` | `billingCycleEnd` |
| `UsedPercent` | `autoPercentUsed` (Models) / `apiPercentUsed` (API) |

`normalizedWindowLabel` already routes `monthly` into the Week/Global summary
column, so both rows show `--` in the `5h` column, exactly as Cursor does today.

### Which percentage for the Models row

Cursor's own UI shows `totalPercentUsed` when Auto is the selected model
(`autoModelSelectedDisplayMessage`), not `autoPercentUsed`. lazyagent uses
`autoPercentUsed` instead: two separate rows are only coherent over disjoint
pools, and using the total for the Models row would double-count API spend
already shown by the API row. The `Note` states this explicitly so the
divergence from Cursor's dashboard is not mistaken for a bug.

## Source and Note lines

```
Cursor Models — Source: 5.2% of the plan's included Auto/Composer allowance (Ultra); billing cycle 25 Jul – 25 Aug
Cursor API    — Source: 41.5% of the plan's included API allowance (Ultra); billing cycle 25 Jul – 25 Aug
```

Percentages are rendered to one decimal, and cycle bounds use the existing
`2 Jan` day-and-month formatting already used for reset times, in local time.

No dollar amounts: the endpoint does not expose the per-pool split in currency,
and `plan.limit` is not the matching denominator (see Background), so rendering
it would mislead.

`Note` follows the existing per-provider pattern — names the undocumented
endpoint, states that it is read on explicit invocation only, and warns it may
break without notice. The Models note adds the `autoPercentUsed` vs
`totalPercentUsed` clarification.

## Skip and error semantics

| Condition | Behaviour |
|-----------|-----------|
| No token in `state.vscdb` | `errAgentNotInstalled` — unchanged, from `cursor.ReadAuth()` |
| `isUnlimited: true` | `errAgentUnavailable` — no percentage to pace against |
| No enabled `plan` block in either usage block | `errAgentUnavailable`, actionable message |
| 401 / 403 | Existing "session token rejected, sign in again" message |

Both rows are skipped together; under `--agent all` the skip is silent, under
`--agent cursor` the wrapped message is printed. The "signed in but on an
unmapped plan" case disappears, since Cursor now supplies the percentage for
every plan.

## Testing

`cursor_test.go` is rewritten around a pure mapping function from the decoded
payload to `[]Report`, keeping the existing no-network testing style. A redacted
real response is added under `internal/limits/testdata/`.

Cases: individual account (two reports, correct percentages, correct window
bounds and order); `isUnlimited: true` → `errAgentUnavailable`;
`plan.enabled: false` → `errAgentUnavailable`; empty `individualUsage` with a
populated `teamUsage` → falls back; both usage blocks empty →
`errAgentUnavailable`; malformed `billingCycleStart` / `billingCycleEnd` → hard
error naming the field.

`internal/ui/limits_test.go` is updated for the new provider names.

## Documentation

- `docs/maintenance/limits.md` — rewrite the Cursor source, budget, skip-case
  and caveat sections; drop `CURSOR_INCLUDED_USD` from the env table; update the
  sample summary table to show both rows.
- `docs/usage/cli.md` — remove the `CURSOR_INCLUDED_USD` row.
- `docs/reference/roadmap.md` — update the Cursor bullet to the new endpoint.
- `internal/limits/run.go` — package doc comment and `--help` text.
- `README.md` — the `lazyagent limits` bullet mentions Cursor's monthly API
  usage; update to both pools.

## Out of scope

- **No on-demand row.** `onDemand.used` / `onDemand.limit` (the `hardLimit`
  spending cap above the included budget) is a third concept and was not
  requested.
- **No `--agent cursor-models` / `--agent cursor-api` sub-selectors.**
- **No fallback to the old computation** if `/api/usage-summary` fails. The old
  path is known to produce the wrong denominator, so keeping it alive would
  double the code paths to maintain in exchange for a wrong number.

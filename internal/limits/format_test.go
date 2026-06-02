package limits

import (
	"strings"
	"testing"
	"time"
)

func TestElapsedPercent(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name          string
		windowMinutes int
		resetsAt      time.Time
		want          float64
	}{
		{"start of window", 300, now.Add(300 * time.Minute), 0},
		{"middle of window", 300, now.Add(150 * time.Minute), 50},
		{"end of window", 300, now, 100},
		{"already past reset", 300, now.Add(-10 * time.Minute), 100},
		{"reset further than window (clamp)", 300, now.Add(400 * time.Minute), 0},
		{"7-day, half consumed", 10080, now.Add(5040 * time.Minute), 50},
		{"zero window minutes", 0, now.Add(time.Hour), 0},
		{"zero reset time", 300, time.Time{}, 0},
	}
	for _, c := range cases {
		got := elapsedPercent(c.windowMinutes, c.resetsAt, now)
		if abs(got-c.want) > 0.01 {
			t.Errorf("%s: got %.3f, want %.3f", c.name, got, c.want)
		}
	}
}

func TestClassifyPace(t *testing.T) {
	cases := []struct {
		name    string
		used    float64
		elapsed float64
		wantP   pace
	}{
		{"window just opened (unknown)", 0.5, 0.5, paceUnknown},
		{"linear", 50, 50, paceOnTrack},
		{"slightly under (still on track)", 48, 50, paceOnTrack},
		{"slightly over (still on track)", 56, 50, paceOnTrack},
		{"clearly under", 20, 50, paceUnder},
		{"clearly over", 70, 50, paceOver},
		{"empty consumption far into window", 0, 80, paceUnder},
		{"full consumption early", 90, 10, paceOver},
	}
	for _, c := range cases {
		_, gotP := classifyPace(c.used, c.elapsed)
		if gotP != c.wantP {
			t.Errorf("%s: got pace=%d, want %d", c.name, gotP, c.wantP)
		}
	}
}

func TestPaceForWindowUsesDetailedBoundary(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	window := Window{
		Label:         "7-day",
		WindowMinutes: 1000,
		UsedPercent:   89,
		ResetsAt:      now.Add(227 * time.Minute), // 77.3% elapsed, ratio just over 1.15.
	}
	report := Report{Windows: []Window{window}}

	wp := paceForWindow(window, now)
	if wp.pace != paceOver {
		t.Fatalf("paceForWindow() pace = %v, want paceOver (ratio %.4f, elapsed %.1f)", wp.pace, wp.ratio, wp.elapsedPercent)
	}

	cell, _, p, _ := summaryCellContent(report, summaryWeekGlobal, now)
	if p != wp.pace {
		t.Fatalf("summary pace = %v, want detailed pace %v", p, wp.pace)
	}
	if cell != "89.0% used / 77.3% exp" {
		t.Fatalf("summary cell = %q, want %q", cell, "89.0% used / 77.3% exp")
	}
}

func TestHumanDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Minute, "30m"},
		{90 * time.Minute, "1h 30m"},
		{4*time.Hour + 23*time.Minute, "4h 23m"},
		{25 * time.Hour, "1d 1h"},
		{5*24*time.Hour + 3*time.Hour, "5d 3h"},
		{0, "0m"},
		{-time.Hour, "0m"},
	}
	for _, c := range cases {
		got := humanDuration(c.d)
		if got != c.want {
			t.Errorf("humanDuration(%v): got %q, want %q", c.d, got, c.want)
		}
	}
}

func TestBar(t *testing.T) {
	cases := []struct {
		name       string
		percent    float64
		width      int
		wantFilled string
		wantEmpty  string
	}{
		{"empty", 0, 10, "", "░░░░░░░░░░"},
		{"full", 100, 10, "██████████", ""},
		{"half", 50, 10, "█████", "░░░░░"},
		{"over 100 clamps to full", 150, 10, "██████████", ""},
		{"negative clamps to empty", -5, 10, "", "░░░░░░░░░░"},
	}
	for _, c := range cases {
		gotFilled, gotEmpty := bar(c.percent, c.width)
		if gotFilled != c.wantFilled || gotEmpty != c.wantEmpty {
			t.Errorf("%s: got (%q, %q), want (%q, %q)",
				c.name, gotFilled, gotEmpty, c.wantFilled, c.wantEmpty)
		}
	}
}

func TestRenderSummaryTable(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	reports := []Report{
		{
			Provider: "Claude Code",
			Windows: []Window{
				{Label: "5-hour", WindowMinutes: 300, UsedPercent: 21, ResetsAt: now.Add(180 * time.Minute)},
				{Label: "7-day", WindowMinutes: 7 * 24 * 60, UsedPercent: 23, ResetsAt: now.Add(3 * 24 * time.Hour)},
			},
		},
		{
			Provider: "Codex",
			Windows: []Window{
				{Label: "5-hour", WindowMinutes: 300, UsedPercent: 4, ResetsAt: now.Add(30 * time.Minute)},
				{Label: "7-day", WindowMinutes: 7 * 24 * 60, UsedPercent: 11, ResetsAt: now.Add(4 * 24 * time.Hour)},
			},
		},
		{
			Provider: "Grok",
			Windows: []Window{
				{Label: "monthly", WindowMinutes: 31 * 24 * 60, UsedPercent: 13.875, ResetsAt: now.Add(15 * 24 * time.Hour)},
			},
		},
		{
			Provider: "Kimi Code",
			Windows: []Window{
				{Label: "weekly", WindowMinutes: 7 * 24 * 60, UsedPercent: 20, ResetsAt: now.Add(6 * 24 * time.Hour)},
				{Label: "5-hour", WindowMinutes: 300, UsedPercent: 10, ResetsAt: now.Add(200 * time.Minute)},
			},
		},
	}

	var b strings.Builder
	renderSummaryTable(&b, reports, now)

	got := b.String()
	if strings.Contains(got, "|---") {
		t.Fatalf("summary table should use chatops table rendering, got markdown divider:\n%s", got)
	}
	for _, want := range []string{
		"Agente", "5h", "Week (or Global)",
		"Claude", "21.0% used / 40.0% exp", "23.0% used / 57.1% exp",
		"Codex", "4.0% used / 90.0% exp", "11.0% used / 42.9% exp",
		"Grok", "--", "13.9% used / 51.6% exp",
		"Kimi", "10.0% used / 33.3% exp", "20.0% used / 14.3% exp",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary table missing %q:\n%s", want, got)
		}
	}
}

func TestSummaryAndDetailedRenderSameUsedPercentFromSameReport(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	report := Report{
		Provider: "Codex",
		Windows: []Window{
			{Label: "5-hour", WindowMinutes: 300, UsedPercent: 7, ResetsAt: now.Add(240 * time.Minute)},
			{Label: "7-day", WindowMinutes: 7 * 24 * 60, UsedPercent: 89, ResetsAt: now.Add(36*time.Hour + 24*time.Minute)},
		},
	}

	var summary strings.Builder
	renderSummaryTable(&summary, []Report{report}, now)
	if !strings.Contains(summary.String(), "89.0% used / 78.3% exp") {
		t.Fatalf("summary should show Codex 7-day used percent from report:\n%s", summary.String())
	}

	var detailed strings.Builder
	renderReport(&detailed, report, now)
	if !strings.Contains(detailed.String(), "Used:      89.0%") {
		t.Fatalf("detailed should show the same Codex 7-day used percent from report:\n%s", detailed.String())
	}
}

func TestSummaryCellSeverity(t *testing.T) {
	cases := []struct {
		name string
		used float64
		pace pace
		want summarySeverity
	}{
		{"low usage under pace is green", 4, paceUnder, sevUnder},
		{"low usage on track is default", 70, paceOnTrack, sevDefault},
		{"over pace flags danger even at low usage", 50, paceOver, sevDanger},
		{"high absolute usage warns regardless of on-track pace", 89, paceOnTrack, sevWarn},
		{"very high absolute usage is danger", 95, paceOnTrack, sevDanger},
		{"absolute warn threshold beats a slow pace", 76, paceUnder, sevWarn},
		{"absolute danger threshold beats a slow pace", 92, paceUnder, sevDanger},
	}
	for _, c := range cases {
		if got := summaryCellSeverity(c.used, c.pace); got != c.want {
			t.Errorf("%s: summaryCellSeverity(%.1f, %v) = %v, want %v", c.name, c.used, c.pace, got, c.want)
		}
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

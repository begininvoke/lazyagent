package limits

import (
	"testing"
	"time"
)

func TestBuildView(t *testing.T) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	reports := []Report{
		{
			Provider: "Claude Code",
			Source:   "source note",
			Note:     "disclaimer",
			Windows: []Window{
				// 5h window: 50% used, reset in 2.5h => 50% elapsed => on track.
				{Label: "5-hour", WindowMinutes: 300, UsedPercent: 50, ResetsAt: now.Add(150 * time.Minute)},
				// 7d window: 95% used => danger severity.
				{Label: "7-day", WindowMinutes: 7 * 24 * 60, UsedPercent: 95, ResetsAt: now.Add(24 * time.Hour)},
			},
		},
	}

	v := BuildView(reports, now)

	if !v.Available {
		t.Fatal("View.Available = false, want true")
	}
	if len(v.Reports) != 1 || len(v.Summary) != 1 {
		t.Fatalf("Reports=%d Summary=%d, want 1/1", len(v.Reports), len(v.Summary))
	}

	fiveH := v.Reports[0].Windows[0]
	if fiveH.ExpectedPercent < 49 || fiveH.ExpectedPercent > 51 {
		t.Errorf("5h ExpectedPercent = %.1f, want ~50", fiveH.ExpectedPercent)
	}
	if !fiveH.PaceKnown || fiveH.PaceLabel != "on track" {
		t.Errorf("5h pace = %q known=%v, want 'on track' true", fiveH.PaceLabel, fiveH.PaceKnown)
	}
	if fiveH.UsedSeverity != SevInfo {
		t.Errorf("5h UsedSeverity = %q, want %q", fiveH.UsedSeverity, SevInfo)
	}
	if fiveH.ResetRelative == "" || fiveH.ResetUnix == 0 {
		t.Errorf("5h reset not populated: rel=%q unix=%d", fiveH.ResetRelative, fiveH.ResetUnix)
	}

	sevenD := v.Reports[0].Windows[1]
	if sevenD.UsedSeverity != SevDanger {
		t.Errorf("7d UsedSeverity = %q, want %q", sevenD.UsedSeverity, SevDanger)
	}

	row := v.Summary[0]
	if row.Provider != "Claude" {
		t.Errorf("summary provider = %q, want Claude", row.Provider)
	}
	if !row.FiveHour.Present || !row.WeekGlobal.Present {
		t.Errorf("summary cells not present: 5h=%v week=%v", row.FiveHour.Present, row.WeekGlobal.Present)
	}
	if row.WeekGlobal.Severity != SevDanger {
		t.Errorf("week severity = %q, want %q", row.WeekGlobal.Severity, SevDanger)
	}
}

func TestBuildViewEmpty(t *testing.T) {
	v := BuildView(nil, time.Now())
	if v.Available {
		t.Fatal("empty View.Available = true, want false")
	}
	if len(v.Reports) != 0 || len(v.Summary) != 0 {
		t.Fatalf("empty view not empty: reports=%d summary=%d", len(v.Reports), len(v.Summary))
	}
}

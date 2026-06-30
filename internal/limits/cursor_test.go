package limits

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

// makeJWT builds a minimal unsigned JWT carrying the given `sub` claim, so we can
// exercise cursorUserIDFromToken without a real Cursor token.
func makeJWT(sub string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"` + sub + `"}`))
	return header + "." + payload + ".sig"
}

func TestCursorUserIDFromToken(t *testing.T) {
	t.Run("strips the auth0/workos prefix before the pipe", func(t *testing.T) {
		got, err := cursorUserIDFromToken(makeJWT("auth0|user_01J7B053"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "user_01J7B053" {
			t.Fatalf("got %q, want %q", got, "user_01J7B053")
		}
	})

	t.Run("returns sub verbatim when there is no pipe", func(t *testing.T) {
		got, err := cursorUserIDFromToken(makeJWT("user_plain"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "user_plain" {
			t.Fatalf("got %q, want %q", got, "user_plain")
		}
	})

	t.Run("errors on a non-JWT string", func(t *testing.T) {
		if _, err := cursorUserIDFromToken("not-a-jwt"); err == nil {
			t.Fatal("expected an error for a malformed token, got nil")
		}
	})
}

func TestCursorSpendByPool(t *testing.T) {
	resp := &cursorAggResponse{
		Aggregations: []cursorAgg{
			{ModelIntent: "claude-opus-4-8-thinking-high", TotalCents: 8000, Tier: 1},
			{ModelIntent: "composer-2.5-fast", TotalCents: 1774, Tier: 2},
			{ModelIntent: "grok-4.3", TotalCents: 5, Tier: 1},
		},
		TotalCostCents: 9779,
	}
	apiCents, autoCents := cursorSpendByPool(resp)
	if apiCents != 8005 {
		t.Errorf("apiCents = %v, want 8005 (tier-1 models)", apiCents)
	}
	if autoCents != 1774 {
		t.Errorf("autoCents = %v, want 1774 (tier-2 composer)", autoCents)
	}
}

func TestCursorIncludedUSD(t *testing.T) {
	cases := []struct {
		membership string
		want       float64
		wantOK     bool
	}{
		{"ultra", 400, true},
		{"pro", 20, true},
		{"pro_plus", 70, true},
		{"PRO", 20, true}, // case-insensitive
		{"enterprise", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		got, ok := cursorIncludedUSD(c.membership)
		if got != c.want || ok != c.wantOK {
			t.Errorf("cursorIncludedUSD(%q) = (%v, %v), want (%v, %v)", c.membership, got, ok, c.want, c.wantOK)
		}
	}
}

func TestCursorIncluded(t *testing.T) {
	t.Run("known paid plan resolves to its budget", func(t *testing.T) {
		t.Setenv("CURSOR_INCLUDED_USD", "")
		usd, plan, err := cursorIncluded("ultra")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if usd != 400 || plan != "Ultra" {
			t.Fatalf("got (%v, %q), want (400, \"Ultra\")", usd, plan)
		}
	})

	t.Run("env override wins over the plan table", func(t *testing.T) {
		t.Setenv("CURSOR_INCLUDED_USD", "123.5")
		usd, _, err := cursorIncluded("ultra")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if usd != 123.5 {
			t.Fatalf("usd = %v, want 123.5", usd)
		}
	})

	t.Run("unknown plan is unavailable, not a hard error", func(t *testing.T) {
		t.Setenv("CURSOR_INCLUDED_USD", "")
		_, _, err := cursorIncluded("business")
		if !errors.Is(err, errAgentUnavailable) {
			t.Fatalf("err = %v, want errAgentUnavailable", err)
		}
	})

	t.Run("free plan has no budget to pace, so it is unavailable", func(t *testing.T) {
		t.Setenv("CURSOR_INCLUDED_USD", "")
		_, _, err := cursorIncluded("free")
		if !errors.Is(err, errAgentUnavailable) {
			t.Fatalf("err = %v, want errAgentUnavailable", err)
		}
	})
}

func TestCursorBillingCycle(t *testing.T) {
	t.Run("adds one month for a mid-month anchor", func(t *testing.T) {
		start := time.Date(2026, 5, 25, 13, 40, 53, 0, time.UTC)
		_, end := cursorBillingCycle(start)
		want := time.Date(2026, 6, 25, 13, 40, 53, 0, time.UTC)
		if !end.Equal(want) {
			t.Fatalf("end = %v, want %v", end, want)
		}
	})

	t.Run("clamps to the last day when the next month is shorter", func(t *testing.T) {
		start := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
		_, end := cursorBillingCycle(start)
		want := time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)
		if !end.Equal(want) {
			t.Fatalf("end = %v, want %v", end, want)
		}
	})
}

func TestCursorUsageToReport(t *testing.T) {
	cycleStart := time.Date(2026, 5, 25, 13, 40, 53, 0, time.UTC)
	cycleEnd := time.Date(2026, 6, 25, 13, 40, 53, 0, time.UTC)
	r := cursorUsageToReport(cursorUsage{
		APISpendUSD:  152.64,
		AutoSpendUSD: 17.74,
		IncludedUSD:  400,
		Plan:         "Ultra",
		CycleStart:   cycleStart,
		CycleEnd:     cycleEnd,
	})

	if r.Provider != "Cursor" {
		t.Errorf("Provider = %q, want %q", r.Provider, "Cursor")
	}
	if len(r.Windows) != 1 {
		t.Fatalf("got %d windows, want exactly 1", len(r.Windows))
	}
	w := r.Windows[0]
	if !w.ResetsAt.Equal(cycleEnd) {
		t.Errorf("ResetsAt = %v, want %v", w.ResetsAt, cycleEnd)
	}
	if w.WindowMinutes <= 0 {
		t.Errorf("WindowMinutes = %d, want > 0", w.WindowMinutes)
	}
	wantPct := 152.64 / 400 * 100
	if diff := w.UsedPercent - wantPct; diff > 0.01 || diff < -0.01 {
		t.Errorf("UsedPercent = %v, want ~%v", w.UsedPercent, wantPct)
	}
	if !isGlobalWindow(w) {
		t.Errorf("monthly window should be classified as global, got label %q / %d min", w.Label, w.WindowMinutes)
	}
	if r.Note == "" {
		t.Error("expected a non-empty disclaimer Note")
	}
}

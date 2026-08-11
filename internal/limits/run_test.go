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

//go:build !notray

package tray

import (
	"testing"

	"github.com/illegalstudio/lazyagent/internal/demo"
)

func TestRunProvider_DemoWins(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if _, ok := runProvider(true, "all").(demo.Provider); !ok {
		t.Errorf("demoMode=true must return demo.Provider (dead since 628cd84)")
	}
	if _, ok := runProvider(true, "claude").(demo.Provider); !ok {
		t.Errorf("demoMode wins over --agent")
	}
	if p := runProvider(false, "all"); p == nil {
		t.Errorf("live mode must return a provider")
	} else if _, ok := p.(demo.Provider); ok {
		t.Errorf("live mode must not return the demo provider")
	}
}

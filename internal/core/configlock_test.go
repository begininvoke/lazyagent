package core

import (
	"sync"
	"testing"
)

func TestUpdateConfig_AppliesMutation(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := UpdateConfig(func(c *Config) { c.CardDensity = "rich" }); err != nil {
		t.Fatal(err)
	}
	if got := LoadConfig().CardDensity; got != "rich" {
		t.Errorf("CardDensity = %q, want \"rich\"", got)
	}
}

func TestUpdateConfig_ConcurrentWritersLoseNoUpdates(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// Seed a valid starting width.
	if err := UpdateConfig(func(c *Config) { c.DetailWidth = 300 }); err != nil {
		t.Fatal(err)
	}

	// 100 concurrent read-modify-write increments. Without a cross-fd
	// lock around the whole load-mutate-save sequence, interleaved
	// writers overwrite each other and the final count comes up short.
	const n = 100
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- UpdateConfig(func(c *Config) { c.DetailWidth++ })
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	if got := LoadConfig().DetailWidth; got != 300+n {
		t.Errorf("DetailWidth = %d, want %d (lost %d updates)", got, 300+n, 300+n-got)
	}
}

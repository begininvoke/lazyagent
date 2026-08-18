package core

import (
	"encoding/json"
	"os"
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

func TestReadConfig_DoesNotTouchDisk(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg, needsSave := readConfig()
	if !needsSave {
		t.Errorf("needsSave = false for a missing config file, want true")
	}
	if cfg.APISalt == "" {
		t.Errorf("APISalt not backfilled in memory")
	}
	if _, err := os.Stat(ConfigPath()); !os.IsNotExist(err) {
		t.Errorf("readConfig wrote to disk: %v", err)
	}
}

func TestLoadConfig_FirstRunPersistsMatchingSalt(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cfg := LoadConfig()
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		t.Fatalf("config not persisted on first run: %v", err)
	}
	var onDisk Config
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatal(err)
	}
	if cfg.APISalt == "" || onDisk.APISalt != cfg.APISalt {
		t.Errorf("salt mismatch: returned %q, on disk %q", cfg.APISalt, onDisk.APISalt)
	}
}

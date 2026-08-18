package core

import (
	"errors"
	"os"
	"path/filepath"
)

// UpdateConfig applies mutate to the freshest on-disk config and saves the
// result, holding an exclusive advisory lock on a sibling lock file for the
// whole load-mutate-save sequence. Every explicit config writer must go
// through it: a bare LoadConfig/SaveConfig pair races with other writers —
// across goroutines AND across processes (GUI preferences vs a concurrent
// `lazyagent-cli passphrase`, for example) — and the last writer silently
// clobbers the other's fields.
func UpdateConfig(mutate func(*Config)) error {
	dir := ConfigDir()
	if dir == "" {
		return errors.New("config directory unavailable")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "config.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := flockExclusive(lock); err != nil {
		return err
	}
	defer flockUnlock(lock)

	// readConfig, not LoadConfig: LoadConfig routes its backfill save
	// through UpdateConfig, so calling it here would recurse. readConfig
	// recomputes any needed backfill in memory and it gets persisted with
	// this save, under the same lock.
	cfg, _ := readConfig()
	mutate(&cfg)
	return SaveConfig(cfg)
}

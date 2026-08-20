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
// `lazyagent passphrase`, for example) — and the last writer silently
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

// PersistAPIAuth saves the API passphrase (empty = keep whatever is on
// disk) and ensures a salt exists, computing BOTH on the freshest config
// under the lock. It returns the persisted passphrase and salt so callers
// derive tokens from what actually landed on disk. Callers must not write
// or derive from values captured in an earlier LoadConfig snapshot: that
// reintroduces the stale-overwrite race the lock exists to prevent (a
// concurrent rotation would be reverted, or the runtime token would be a
// stale-passphrase/fresh-salt hybrid nobody can derive).
func PersistAPIAuth(passphrase string) (string, string, error) {
	var pass, salt string
	err := UpdateConfig(func(c *Config) {
		if passphrase != "" {
			c.APIPassphrase = passphrase
		}
		EnsureAPISalt(c)
		pass = c.APIPassphrase
		salt = c.APISalt
	})
	return pass, salt, err
}

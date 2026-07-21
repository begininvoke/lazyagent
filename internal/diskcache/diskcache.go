// Package diskcache provides small, shared primitives for persisting
// advisory discovery caches to disk between one-shot CLI runs. Callers
// (internal/model's SessionCache, internal/codex's and internal/claude's
// CWDIndex) own their own on-disk shape and format-versioning; this package
// only handles the mechanics of getting bytes on and off disk safely.
package diskcache

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// AtomicWriteJSON marshals v to JSON and writes it to path atomically: the
// data is first written to a unique temporary file in the same directory as
// path (so the final rename is same-filesystem and therefore atomic), then
// renamed into place. A reader opening path concurrently always sees either
// the previous complete file or the new complete file, never a torn one. If
// any step fails, the temp file is removed and path is left untouched. The
// final file is created with 0600 permissions, since these caches can
// contain transcript snippets.
func AtomicWriteJSON(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-"+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	committed = true
	return nil
}

// ReadJSON reads and unmarshals the JSON file at path into v. It returns an
// error for any failure (missing file, unreadable, malformed JSON) and
// leaves v untouched on a top-level decode failure. Callers implementing
// "advisory cache, never fail discovery" semantics should treat any
// non-nil error as "no persisted cache" and continue cold.
func ReadJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

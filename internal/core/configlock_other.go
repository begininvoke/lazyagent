//go:build !unix

package core

import "os"

// No advisory locking on this platform (no shipped builds use it);
// UpdateConfig still serializes nothing here but keeps compiling.
func flockExclusive(*os.File) error { return nil }

func flockUnlock(*os.File) error { return nil }

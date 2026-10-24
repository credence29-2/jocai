//go:build !windows

package store

// Outside Windows there's no DPAPI. The file is still only readable by the
// owner (0600). This build is for development.
func protect(b []byte) ([]byte, error)   { return b, nil }
func unprotect(b []byte) ([]byte, error) { return b, nil }

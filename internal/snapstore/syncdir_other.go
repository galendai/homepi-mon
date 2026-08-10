//go:build !unix

package snapstore

// syncDir is a no-op on non-Unix platforms. The persistent kiosk target is
// Linux; keeping this implementation explicit preserves cross-compilation for
// diagnostic builds on other systems.
func syncDir(path string) error { return nil }

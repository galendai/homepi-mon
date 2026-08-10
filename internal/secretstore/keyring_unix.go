//go:build !windows && !darwin

package secretstore

// platform returns the GOOS identifier used to label the active backend.
func platform() string {
	return "linux"
}

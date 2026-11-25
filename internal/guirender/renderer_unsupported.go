//go:build !linux || !cgo

package guirender

// NewRenderer reports that the official LVGL framebuffer backend is unavailable.
func NewRenderer(fbDevice string) (Renderer, error) {
	return nil, errUnsupported
}

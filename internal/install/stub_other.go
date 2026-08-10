//go:build !darwin && !linux && !windows

package install

import "context"

func platformInstall(_ context.Context, _ Config) error { return ErrUnsupported }
func platformStart(_ context.Context) error             { return ErrUnsupported }
func platformStop(_ context.Context) error              { return ErrUnsupported }
func platformStatus(_ context.Context) (Report, error)  { return Report{}, ErrUnsupported }
func platformUninstall(_ context.Context) error         { return ErrUnsupported }

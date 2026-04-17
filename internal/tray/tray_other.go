//go:build !windows

package tray

func run(cfg Config) error { return ErrNotSupported }
func stop()                 {}

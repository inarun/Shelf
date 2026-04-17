//go:build !windows

package note

func isWindowsSkipPerm() bool { return false }

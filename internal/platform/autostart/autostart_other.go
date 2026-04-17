//go:build !windows

package autostart

func (a *Autostart) enable() error            { return ErrNotSupported }
func (a *Autostart) disable() error           { return ErrNotSupported }
func (a *Autostart) status() (bool, string, error) {
	return false, "", ErrNotSupported
}

// Package autostart registers and removes a Shelf "start with Windows"
// entry under HKCU\Software\Microsoft\Windows\CurrentVersion\Run.
//
// # Why HKCU\...\Run
//
// SKILL.md §Open questions calls for "the least-privileged option that
// works." The HKCU Run key is the canonical Windows per-user autostart
// mechanism:
//
//   - No admin / elevation required.
//   - Unique to the current user; cannot affect other accounts on the
//     machine.
//   - Honoured by explorer.exe on interactive login.
//   - No scheduler, no service, no startup folder .lnk handling.
//
// # Safety
//
// The value is always a single stringified command that points at the
// binary the current process was launched from. It never contains user
// input. Callers are expected to pass an argv already properly quoted;
// we do not re-interpret or re-quote it.
package autostart

import "errors"

// AppName is the registry value name used under the Run key. A single
// stable name means re-registration overwrites rather than duplicating.
const AppName = "Shelf"

// ErrNotSupported is returned when this package is used on a platform
// that does not have a Windows Registry. The unix build stub returns
// this from every method.
var ErrNotSupported = errors.New("autostart: not supported on this platform")

// Autostart is a small handle bundling the app name (the registry
// value name) and the command line to register. Construct one with
// New and call Enable / Disable / Status.
type Autostart struct {
	name    string
	command string
}

// New validates inputs and returns a handle. An empty name or empty
// command is always an error — silently writing an empty Run value
// would be a footgun for the user.
func New(name, command string) (*Autostart, error) {
	if name == "" {
		return nil, errors.New("autostart: name is empty")
	}
	if command == "" {
		return nil, errors.New("autostart: command is empty")
	}
	return &Autostart{name: name, command: command}, nil
}

// Enable writes the Run entry. If an entry with the same name already
// exists it is overwritten.
func (a *Autostart) Enable() error { return a.enable() }

// Disable removes the Run entry. Removing an entry that does not exist
// is not an error — Disable is idempotent.
func (a *Autostart) Disable() error { return a.disable() }

// Status returns whether a Run entry with this name exists, and if so
// the command it points to. A nil error with enabled=false means the
// entry is not present.
func (a *Autostart) Status() (enabled bool, command string, err error) {
	return a.status()
}

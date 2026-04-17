package sync

// EventKind is the typed category of a filesystem change applied to the
// index. The watcher emits these; a full scan never does.
type EventKind int

const (
	// EventCreate signals a newly-seen .md file.
	EventCreate EventKind = iota + 1
	// EventWrite signals an existing file was modified.
	EventWrite
	// EventRemove signals a file disappeared.
	EventRemove
	// EventRename signals a file was renamed (distinct from Remove+Create
	// when the watcher can detect it). On Windows atomic renames this
	// often surfaces as a Remove followed by a Create; the syncer treats
	// either shape consistently.
	EventRename
)

// Event is a single filesystem change to apply to the index.
type Event struct {
	Kind EventKind
	Path string // absolute path to the affected file
	// OldPath is set only for EventRename, pointing at the pre-rename
	// location so the index can delete the old row.
	OldPath string
}

// Report is the outcome of a FullScan.
type Report struct {
	Scanned int         // .md files encountered on the walk
	Indexed int         // files re-read and upserted
	Skipped int         // files whose stat matched the index (cheap path)
	Deleted int         // index rows removed because the file vanished
	Errors  []FileError // per-file errors; scan continues past these
}

// FileError reports a per-file error — parse, stat, or index failure.
// The scan surfaces these in Report.Errors instead of aborting the
// whole operation, because the vault is truth and a single malformed
// file must not make the rest of the library invisible.
type FileError struct {
	Filename string
	Err      error
}

func (e FileError) Error() string {
	return e.Filename + ": " + e.Err.Error()
}

func (e FileError) Unwrap() error { return e.Err }

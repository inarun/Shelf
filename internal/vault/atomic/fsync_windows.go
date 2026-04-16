//go:build windows

package atomic

// fsyncDir is a no-op on Windows: the platform's File.Sync does not
// operate on directories, and os.Rename (via MoveFileExW) is the unit of
// atomicity. Durability of the rename itself is at the filesystem's
// discretion, which is acceptable for Shelf's data model — every vault
// file is reconstructible from its Markdown source, and the SQLite index
// is a rebuildable cache.
func fsyncDir(_ string) error {
	return nil
}

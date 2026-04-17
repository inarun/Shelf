// Package sync reconciles the vault's .md book files into the SQLite
// index. FullScan walks the books directory, compares each file's stat
// to what's already in the index (short-circuiting unchanged files),
// and deletes rows for files that have vanished. Apply dispatches a
// single filesystem event to the appropriate index update path.
//
// The reconciler is index-owning: the vault is truth and SQLite is a
// rebuildable cache. A file that fails to parse doesn't fail the scan
// — it accumulates into Report.Errors and the rest of the vault is
// still indexed.
package sync

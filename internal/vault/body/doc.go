// Package body parses and serializes the Markdown body of a book note.
//
// A body is modelled as a slice of Blocks where each block carries both
// the raw source bytes from the parse input and a typed parsed view.
// Blocks that have not been mutated since parse serialize back to their
// raw bytes verbatim, which guarantees byte-for-byte round-trip for any
// unmodified input — including unrecognized ##-sections, CRLF line
// endings, and odd spacing the user cares about.
//
// Mutations go through helper methods that set the block's dirty flag
// and never touch Raw. Serialize then regenerates a canonical form for
// dirty blocks while leaving clean blocks untouched, so a rating change
// writes only the rating region and leaves the rest of the file byte-
// identical.
package body

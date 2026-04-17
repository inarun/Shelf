// Package covers is the content-addressed cache for book cover images.
// Cache filenames are sha256(providerKey) + ".jpg"|".png" so identical
// refs from the same provider dedupe automatically; distinct providers
// never collide because the provider name is part of the hashed key.
//
// The cache lives under {data.directory}/covers/ per SKILL.md §Configuration.
// Every write goes through internal/vault/atomic, every path resolves
// through internal/vault/paths.ValidateWithinRoot. The cache never
// deletes — Session 6 treats covers as write-once; future work may
// add an LRU or staleness sweep.
//
// The cover reference stored in a note's frontmatter is the URL-shaped
// string "/covers/<hash>.<ext>" so the web UI can use it directly as a
// same-origin href under the default CSP (img-src 'self').
package covers

// Package openlibrary is the Open Library implementation of
// metadata.Provider. It hits openlibrary.org for bibliographic data
// and covers.openlibrary.org for cover images. Only those two hosts
// are ever contacted; redirects to anything else are rejected.
//
// Every outbound request uses a bounded context, a 15s timeout, a
// fixed User-Agent, and a per-response size cap. Calls happen only
// when a user action triggers them (add-book lookup/search, explicit
// "fetch cover" button) per SKILL.md §Core Invariants #8.
package openlibrary

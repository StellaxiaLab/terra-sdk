// Package pkgformat holds the module package format that publishers, a
// marketplace and a Terra node must judge identically: the Manifest v2 model
// and its validation, the legacy v1 manifest and its normalisation, the
// configuration schema, canonical permission strings, and the .tmod archive
// reader/writer with its size limits.
//
// Stability: v0.x. See docs/policy/versioning.md. Standard library plus
// modulert (for Readiness); this package does not import trust.
package pkgformat

// Package trust holds the signing and verification formats that a package
// marketplace and a Terra node share: the manifest author signature, the
// counter-signature chain, the market root and key certificates (threshold
// signed), and revocation snapshots, together with their verifiers.
//
// The canonical payload rules are part of the public contract; see
// docs/contracts/market-trust.md. The signed bytes are defined by Go's
// encoding/json behaviour (sorted keys, float64 numbers, HTML escaping), and
// the golden vectors under testdata pin them.
//
// Stability: v0.x. Signed byte formats change only with a documented migration
// (docs/policy/versioning.md). Standard library only; this package imports no
// other SDK package.
package trust

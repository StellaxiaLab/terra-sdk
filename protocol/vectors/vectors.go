// Package vectors holds the golden vectors of the hosted-access contract.
//
// They are the contract a service checks itself against: terra-id signs and
// terra-access verifies with their own code, and both must reach the verdicts
// recorded here. The protocol package's tests read the same files, so a change
// to a vector is a change to the contract and is made here, on purpose.
//
//	terraid/tokens.json        signed tokens, their claims and expected verdicts
//	terraid/jwks.json          the public key set the tokens verify against
//	terraid/key_bundle.json    a Tree key bundle carrying that set
//	terraid/test_key.json      the TEST-ONLY private key; never trust it
//	terraname/vectors.json     name grammar and reference classification
//	terraname/record_vectors.json  dns_label slugs and signed name records
package vectors

import "embed"

// FS is the vector files, at the paths listed in the package comment.
//
//go:embed terraid/*.json terraname/*.json
var FS embed.FS

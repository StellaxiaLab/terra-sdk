// Package conformance is a test kit that checks whether a program honours the
// module-side HTTP contract a Terra host expects: six environment variables,
// one credential header and two control paths.
//
// It knows nothing about Terra's code. The contract is spelled out here as
// literals and exercised over plain HTTP, so it works for a module written in
// any language: the kit launches the module as a child process with a freshly
// issued identity, then plays the host's part of the activation. Go modules
// that use this SDK are checked the same way as everyone else.
//
// The contract itself is written down in docs/contracts/module-host-http-contract.md.
package conformance

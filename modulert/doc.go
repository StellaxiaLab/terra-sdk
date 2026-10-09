// Package modulert holds the wire contract between a hosted module and the
// host: the environment keys, control paths, the credential header, and the DTOs
// both sides serialize (workload identity, handshake, readiness, core
// invocation, SVI sink, gateway delegate, shared roots).
//
// It is the contract subset of Terra's terra-module-runtime package. The
// activation engine, supervisor, manifest handling and registry stay in the
// host and are not part of this SDK. Everything here is standard library only.
package modulert

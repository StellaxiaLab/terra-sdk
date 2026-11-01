// Package protocol holds the wire types Terra shares with code outside its own
// repository:
//
//   - input events, input permissions and virtual-input helpers, shared between
//     a module and the host;
//   - the hosted-access contract — Terra ID tokens and key sets (terraid*.go),
//     `.terra` names and name records (terraname*.go, dnslabel_slug.go), the
//     hosted-access delegation intents (hosted_access_intents.go) and the
//     node-id shape they rely on (noderef.go) — shared between a Tree and the
//     terra-id / terra-access services. Its golden vectors are in the vectors
//     subpackage.
//
// Terra's terra-protocol package re-exports every name here as an alias, so
// there is one definition. Transport, tickets, the rest of delegation and the
// wire protocol stay in the host and are not part of this SDK.
package protocol

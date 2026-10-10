// Package svi holds the SVI (Stellaxia Virtual Interface) data types a module
// exchanges with the host: resource descriptors, endpoints and their states,
// and the checks the host runs on a descriptor before it accepts it
// (ValidateResource, ValidateEndpoint, ParseSchemaRef).
//
// It is a subset of Terra's terra-svi package. Conversion and binding state
// stay in the host and are not part of this SDK.
package svi

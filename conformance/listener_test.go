package conformance_test

import "net"

// newListener binds the exact endpoint an Identity reserved, the way a module
// would after reading TERRA_LOOPBACK_ENDPOINT.
func newListener(endpoint string) (net.Listener, error) { return net.Listen("tcp", endpoint) }

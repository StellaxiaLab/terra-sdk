package resourcize_test

import "net"

func newListener(endpoint string) (net.Listener, error) { return net.Listen("tcp", endpoint) }

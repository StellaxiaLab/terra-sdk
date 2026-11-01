package protocol

import "errors"

// ErrInvalidDelegation marks a delegation request or instruction whose shape a
// child Tree must not act on. The hosted-access validators wrap it, and Terra's
// delegation code re-exports and wraps the same value, so errors.Is and the
// message ("invalid delegation request: ...") a Master puts on the wire are the
// same whichever side produced the error.
var ErrInvalidDelegation = errors.New("invalid delegation request")

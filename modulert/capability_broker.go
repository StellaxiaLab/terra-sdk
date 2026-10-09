package modulert

import "errors"

// Subset of Terra products/common/packages/terra-module-runtime/capability_broker.go:
// only the error a module sees when the host denies a core operation. The broker
// itself is host-side and is not part of this SDK.

// ErrCoreOperationDenied is returned when a module invokes a Core operation its
// manifest declares no permission for (design §15.1 permission intersection on
// the module→core path).
var ErrCoreOperationDenied = errors.New("module is not permitted to invoke this core operation")

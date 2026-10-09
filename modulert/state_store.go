package modulert

import "path/filepath"

// Subset of Terra products/common/packages/terra-module-runtime/state_store.go:
// only where a module's own data lives. The state store is host-side and is not
// part of this SDK.

// DataDirName is the subdirectory of the state root that holds each module's
// own data — the storage a module declares with permissions.storage:
// ["module-data"] and writes into itself.
//
// It sits beside StateDirName because the two answer different questions about
// the same module and only the pair makes either legible: module-state is the
// Keeper's ledger about a module, module-data is the module's own contents.
//
// A module used to compute this path from $HOME for itself, which meant it did
// not follow the product's data directory: moving data_dir moved the ledger and
// left the data behind, and two hosts under one account shared one directory
// per module — three of them once wrote over each other's contents.
const DataDirName = "module-data"

// ModuleDataDir is where one module's own data lives under a product's data
// root. Callers hand it the same root the state store uses.
func ModuleDataDir(root, moduleID string) string {
	return filepath.Join(root, DataDirName, moduleID)
}

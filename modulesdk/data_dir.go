package modulesdk

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	modulert "github.com/StellaxiaLab/terra-sdk/modulert"
)

// DataDirEnv is where the host names this module's own data directory. It is
// set only for a module whose manifest declares permissions.storage:
// ["module-data"].
const DataDirEnv = "TERRA_MODULE_DATA_DIR"

// DataDir returns the directory this module may write its own data into, and
// creates it if the host has not already.
//
// Prefer this over computing a path from the home directory. A module that
// works out its own location does not follow the product's data directory:
// moving data_dir moves the Keeper's ledger and leaves the module's contents
// behind, and two hosts running under one account share one directory per
// module. Three of them once wrote over each other's contents that way.
//
// The home-directory form is still the fallback, because a module has to keep
// working under a host too old to grant the directory — and because that is
// where existing data already sits, so the fallback is also the migration path:
// nothing moves until a granting host puts the new directory in the
// environment.
func DataDir(moduleID string) (string, error) {
	if granted := strings.TrimSpace(os.Getenv(DataDirEnv)); granted != "" {
		if err := os.MkdirAll(granted, 0o700); err != nil {
			return "", fmt.Errorf("create module data directory: %w", err)
		}
		return granted, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home for module data: %w", err)
	}
	fallback := modulert.ModuleDataDir(filepath.Join(home, ".terra"), moduleID)
	if err := os.MkdirAll(fallback, 0o700); err != nil {
		return "", fmt.Errorf("create module data directory: %w", err)
	}
	return fallback, nil
}

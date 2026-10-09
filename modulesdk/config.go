package modulesdk

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	modulert "github.com/StellaxiaLab/terra-sdk/modulert"
)

// ConfigFileEnv is where the host names this module's effective configuration
// (모듈 설정 설계 §5). It is set only for a module whose manifest declares
// configuration.schema, and only when the host has values to hand over.
const ConfigFileEnv = modulert.ConfigFileEnv

// ErrConfigNotProvided means the host did not hand this process a
// configuration file: the manifest declares no schema, or the host predates
// module configuration. A module that can run on built-in defaults treats it
// as "use the defaults"; one that cannot should exit and say so.
var ErrConfigNotProvided = errors.New("module configuration was not provided (" + ConfigFileEnv + " is not set)")

// ModuleConfig returns this module's effective configuration: the values an operator
// set, with the schema's defaults filled in, already validated by the host.
//
// The file is the host's copy, made for this start. Reading it is all a module
// does — writing it changes nothing the host keeps, and the next start
// replaces it. Values change through the daemon's config operation, which
// restarts the module, so reading once at startup is enough.
func ModuleConfig() (map[string]any, error) {
	path := strings.TrimSpace(os.Getenv(ConfigFileEnv))
	if path == "" {
		return nil, ErrConfigNotProvided
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read module configuration: %w", err)
	}
	var values map[string]any
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, fmt.Errorf("decode module configuration: %w", err)
	}
	if values == nil {
		values = map[string]any{}
	}
	return values, nil
}

// LoadModuleConfig decodes this module's effective configuration into target,
// typically a pointer to a struct whose json tags name the schema's keys.
// Unknown keys are refused so a renamed field fails loudly instead of quietly
// keeping its zero value.
func LoadModuleConfig(target any) error {
	path := strings.TrimSpace(os.Getenv(ConfigFileEnv))
	if path == "" {
		return ErrConfigNotProvided
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read module configuration: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode module configuration: %w", err)
	}
	return nil
}

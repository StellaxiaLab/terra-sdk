package modulert

// Subset of Terra products/common/packages/terra-module-runtime/config_store.go:
// only the environment key a module reads. The config store is host-side and is
// not part of this SDK.

// ConfigFileEnv names the effective configuration file in a module's start
// environment. Only a module whose manifest declares configuration.schema gets
// it, and it points at the effective copy — never at the stored values, so a
// module cannot rewrite what the host validated.
const ConfigFileEnv = "TERRA_MODULE_CONFIG_FILE"

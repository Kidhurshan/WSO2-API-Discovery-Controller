package config

import (
	"os"
	"regexp"
)

// envVarPattern matches ${IDENTIFIER} where IDENTIFIER is a valid POSIX
// shell identifier ([A-Za-z_][A-Za-z0-9_]*).
//
// Only the curly-brace form is recognized. Bare $VAR is intentionally NOT
// expanded so that regular expression patterns containing the end-of-string
// anchor `$` (e.g., "^[0-9]+$" in [discovery.normalization]) work without
// escaping.
var envVarPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// expandEnvVars replaces ${IDENTIFIER} placeholders in the input with the
// values of matching environment variables.
//
// Behavior:
//   - ${VAR} where VAR is set       → replaced with VAR's value
//   - ${VAR} where VAR is unset     → left as the literal string "${VAR}"
//     (so missing variables produce a clear, locatable error rather than
//     silently substituting an empty string)
//   - $VAR (no braces)              → left untouched (regex compatibility)
//   - $ followed by non-identifier  → left untouched
//
// This function is applied to raw configuration file bytes before TOML
// parsing, so any string field in the configuration may reference
// environment variables.
func expandEnvVars(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		// match is "${NAME}"; strip the leading "${" and trailing "}"
		name := match[2 : len(match)-1]
		if value, ok := os.LookupEnv(name); ok {
			return value
		}
		// Preserve the literal placeholder when the env var is unset.
		// This makes misconfiguration easy to spot in logs and config dumps.
		return match
	})
}

package config

import (
	"os"
	"regexp"
	"sort"
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

// unexpandedEnvVars returns the distinct, sorted names of ${VAR} placeholders
// that remain in s after expandEnvVars has run (i.e., env vars that were
// expected by the config but not set in the environment).
//
// TOML comments are stripped before scanning — comments legitimately mention
// ${VAR} as documentation text (e.g., "Replace ${POSTGRES_HOST} with …") and
// must not trigger a fail-fast error.
func unexpandedEnvVars(s string) []string {
	scrubbed := stripTOMLComments(s)
	matches := envVarPattern.FindAllStringSubmatch(scrubbed, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	for _, m := range matches {
		seen[m[1]] = struct{}{}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// stripTOMLComments removes TOML comments (# to end-of-line) from s while
// preserving # characters that appear inside double-quoted strings. This is
// not a full TOML parser — it handles the shapes that appear in ADC's config
// (double-quoted scalar values, no triple-quoted strings, no literal strings
// with ' which don't contain # in our config).
func stripTOMLComments(s string) string {
	var b []byte
	inString := false
	escape := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !inString && c == '#' {
			// Skip to end of line.
			for i < len(s) && s[i] != '\n' {
				i++
			}
			if i < len(s) {
				b = append(b, '\n')
			}
			continue
		}
		if c == '"' && !escape {
			inString = !inString
		}
		escape = !escape && c == '\\'
		b = append(b, c)
	}
	return string(b)
}

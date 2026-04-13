package config

import (
	"testing"
)

func TestExpandEnvVars(t *testing.T) {
	t.Setenv("ADC_TEST_USER", "alice")
	t.Setenv("ADC_TEST_PASSWORD", "s3cr3t")
	t.Setenv("ADC_TEST_EMPTY", "")

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "simple expansion",
			in:   `password = "${ADC_TEST_PASSWORD}"`,
			want: `password = "s3cr3t"`,
		},
		{
			name: "multiple expansions",
			in:   `user = "${ADC_TEST_USER}"` + "\n" + `password = "${ADC_TEST_PASSWORD}"`,
			want: `user = "alice"` + "\n" + `password = "s3cr3t"`,
		},
		{
			name: "unset variable preserved as literal",
			in:   `password = "${ADC_TEST_NOT_SET}"`,
			want: `password = "${ADC_TEST_NOT_SET}"`,
		},
		{
			name: "empty variable expands to empty string",
			in:   `password = "${ADC_TEST_EMPTY}"`,
			want: `password = ""`,
		},
		{
			name: "regex end-of-string anchor preserved (bare $)",
			in:   `pattern = "^[0-9]+$"`,
			want: `pattern = "^[0-9]+$"`,
		},
		{
			name: "regex with $ followed by quote preserved",
			in:   `patterns = ["^v[0-9]+$", "^api$"]`,
			want: `patterns = ["^v[0-9]+$", "^api$"]`,
		},
		{
			name: "bare $VAR form not expanded (no braces)",
			in:   `legacy = "$ADC_TEST_USER"`,
			want: `legacy = "$ADC_TEST_USER"`,
		},
		{
			name: "dollar at end of string preserved",
			in:   `trailing = "abc$"`,
			want: `trailing = "abc$"`,
		},
		{
			name: "dollar followed by digit preserved (not valid identifier start)",
			in:   `weird = "$1"`,
			want: `weird = "$1"`,
		},
		{
			name: "expansion inside larger string",
			in:   `url = "postgres://${ADC_TEST_USER}:${ADC_TEST_PASSWORD}@db:5432/adc"`,
			want: `url = "postgres://alice:s3cr3t@db:5432/adc"`,
		},
		{
			name: "no expansions returns input unchanged",
			in:   `host = "localhost"`,
			want: `host = "localhost"`,
		},
		{
			name: "empty input",
			in:   "",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := expandEnvVars(tc.in)
			if got != tc.want {
				t.Errorf("expandEnvVars() mismatch\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

// TestExpandEnvVars_RegexPatternsFromConfig verifies that all regex patterns
// shipped in the default config.toml survive env var expansion unchanged.
// This is the most important safety check — these patterns are load-bearing
// for path normalization and a silent corruption would break Phase 1.
func TestExpandEnvVars_RegexPatternsFromConfig(t *testing.T) {
	patterns := []string{
		`^v?[0-9]+\.[0-9]+(\.[0-9]+)?$`,
		`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
		`^[0-9a-fA-F]{24,}$`,
		`^[0-9]+$`,
		`^[A-Za-z0-9_-]{20,}={0,2}$`,
		`^[A-Z]{2,5}-[A-Z0-9-]{3,}$`,
		`^(?=.*[0-9])[a-zA-Z0-9]{8,}$`,
		`^v[0-9]+$`,
		`^api$`,
	}

	for _, p := range patterns {
		t.Run(p, func(t *testing.T) {
			got := expandEnvVars(p)
			if got != p {
				t.Errorf("regex pattern corrupted by expansion\n got: %q\nwant: %q", got, p)
			}
		})
	}
}

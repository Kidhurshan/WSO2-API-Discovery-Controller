package discovery

import (
	"regexp"
	"strings"

	"github.com/wso2/adc/internal/config"
	"github.com/wso2/adc/internal/logging"
)

// Normalizer replaces dynamic path segments with {id} placeholders.
type Normalizer struct {
	excludePatterns []*regexp.Regexp
	builtinPatterns []*regexp.Regexp
	userPatterns    []*regexp.Regexp
}

// NewNormalizer compiles regex patterns from config.
func NewNormalizer(cfg config.NormalizationConfig, logger *logging.Logger) *Normalizer {
	n := &Normalizer{
		excludePatterns: compilePatterns(cfg.ExcludePatterns, "exclude", logger),
		builtinPatterns: compilePatterns(cfg.BuiltinPatterns, "builtin", logger),
		userPatterns:    compilePatterns(cfg.UserPatterns, "user", logger),
	}
	return n
}

// Normalize replaces dynamic segments in a path with {id}.
func (n *Normalizer) Normalize(path string) string {
	if path == "" || path == "/" {
		return path
	}

	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	changed := false

	for i, seg := range segments {
		if seg == "" {
			continue
		}

		// Long segments (>255 chars) are almost certainly encoded IDs
		if len(seg) > 255 {
			segments[i] = "{id}"
			changed = true
			continue
		}

		// Check exclude patterns first — if any match, keep as-is
		if matchesAny(seg, n.excludePatterns) {
			continue
		}

		// Check builtin patterns — first match replaces with {id}
		if matchesAny(seg, n.builtinPatterns) {
			segments[i] = "{id}"
			changed = true
			continue
		}

		// Check user patterns
		if matchesAny(seg, n.userPatterns) {
			segments[i] = "{id}"
			changed = true
			continue
		}
	}

	if !changed {
		return path
	}

	return "/" + strings.Join(segments, "/")
}

func matchesAny(s string, patterns []*regexp.Regexp) bool {
	for _, p := range patterns {
		if p.MatchString(s) {
			return true
		}
	}
	return false
}

func compilePatterns(patterns []string, category string, logger *logging.Logger) []*regexp.Regexp {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			logger.Warnw("Invalid regex pattern, skipping",
				"category", category,
				"pattern", p,
				"error", err,
			)
			continue
		}
		compiled = append(compiled, re)
	}
	return compiled
}

package cli

import (
	"fmt"
	"regexp"
)

// slugPattern restricts slugs to safe path segments (no dots, slashes or glob
// metacharacters) so server-controlled slugs cannot escape Codex directories.
var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// checkSlug validates a slug used to build filesystem paths or TOML section
// names.
func checkSlug(slug string) error {
	if !slugPattern.MatchString(slug) {
		return fmt.Errorf("非法 slug %q：只能包含小写字母、数字、- 和 _", slug)
	}
	return nil
}

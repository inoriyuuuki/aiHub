// Package slug provides a conservative slug validator for resource identifiers
// that flow into object keys, zip entries and filesystem paths.
package slug

import "regexp"

// pattern restricts slugs to lowercase letters, digits, '-' and '_'.
var pattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// Valid reports whether slug is safe to use in object keys and paths.
func Valid(slug string) bool { return pattern.MatchString(slug) }

/*
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package logging

import (
	"sort"
	"strings"
)

const RedactedValue = "<redacted>"
const idHintWidth = 8

func HasValue(value string) bool {
	return strings.TrimSpace(value) != ""
}

func RedactString(value string) string {
	if !HasValue(value) {
		return ""
	}
	return RedactedValue
}

// SafeResourceIdentifier preserves non-secret infrastructure identifiers such as OCI resource OCIDs.
func SafeResourceIdentifier(value string) string {
	return strings.TrimSpace(value)
}

func IdentifierHint(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= idHintWidth {
		return trimmed
	}
	return "..." + trimmed[len(trimmed)-idHintWidth:]
}

func SortedMapKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func SortedNestedMapKeys[T any](values map[string]map[string]T) []string {
	keys := make([]string, 0)
	for namespace, nested := range values {
		for key := range nested {
			keys = append(keys, namespace+"/"+key)
		}
	}
	sort.Strings(keys)
	return keys
}

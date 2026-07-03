/*
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package enableautoscaler

import "strings"

// IsBareMetalShape reports whether the provided shape string refers to a Bare Metal shape.
// Bare Metal shapes in OCI use the "BM." prefix, case-insensitive.
func IsBareMetalShape(shape string) bool {
	s := strings.TrimSpace(shape)
	if s == "" {
		return false
	}
	return strings.HasPrefix(strings.ToUpper(s), "BM.")
}

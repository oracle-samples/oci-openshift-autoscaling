/*
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package utils

import "strings"

func EnsureBuildPlatformInFromLine(line string) string {
	if !strings.HasPrefix(line, "FROM ") {
		return line
	}
	if strings.Contains(line, "--platform=") {
		return line
	}
	return strings.Replace(line, "FROM ", "FROM --platform=${BUILDPLATFORM} ", 1)
}

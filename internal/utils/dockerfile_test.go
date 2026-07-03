/*
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package utils

import (
	"strings"
	"testing"
)

func TestEnsureBuildPlatformInFromLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{
			name: "adds platform when missing",
			line: "FROM golang:1.24 AS builder",
			want: "FROM --platform=${BUILDPLATFORM} golang:1.24 AS builder",
		},
		{
			name: "preserves existing platform flag",
			line: "FROM --platform=$BUILDPLATFORM golang:1.24 AS builder",
			want: "FROM --platform=$BUILDPLATFORM golang:1.24 AS builder",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EnsureBuildPlatformInFromLine(tt.line)
			if got != tt.want {
				t.Fatalf("EnsureBuildPlatformInFromLine() = %q, want %q", got, tt.want)
			}
			if strings.Count(got, "--platform=") > 1 {
				t.Fatalf("EnsureBuildPlatformInFromLine() added duplicate platform flags: %q", got)
			}
		})
	}
}

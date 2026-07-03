/*
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package capoci

import (
	"strings"
	"testing"
)

func TestCAPOCICredentialsValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		credentials *CAPOCICredentials
		wantErr     string
	}{
		{
			name: "accepts key based auth with required fields",
			credentials: &CAPOCICredentials{
				TenancyID:            "ocid1.tenancy.oc1..example",
				UserID:               "ocid1.user.oc1..example",
				Region:               "us-phoenix-1",
				Fingerprint:          "11:22:33",
				PrivateKey:           "-----BEGIN PRIVATE KEY-----",
				UseInstancePrincipal: "false",
			},
		},
		{
			name: "accepts instance principal with region only",
			credentials: &CAPOCICredentials{
				Region:               "us-phoenix-1",
				UseInstancePrincipal: "true",
			},
		},
		{
			name: "rejects invalid instance principal flag",
			credentials: &CAPOCICredentials{
				UseInstancePrincipal: "sometimes",
			},
			wantErr: `invalid OCI_USE_INSTANCE_PRINCIPAL "sometimes": must be true or false`,
		},
		{
			name: "rejects missing region for instance principal",
			credentials: &CAPOCICredentials{
				UseInstancePrincipal: "true",
			},
			wantErr: "instance-principal auth requires OCI_REGION",
		},
		{
			name: "rejects missing key based fields",
			credentials: &CAPOCICredentials{
				TenancyID:            "ocid1.tenancy.oc1..example",
				UseInstancePrincipal: "false",
			},
			wantErr: "key-based auth requires OCI_USER_ID, OCI_REGION, OCI_CREDENTIALS_FINGERPRINT, OCI_CREDENTIALS_KEY",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.credentials.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() error = nil, want %q", tt.wantErr)
			}
			if err.Error() != tt.wantErr {
				t.Fatalf("Validate() error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestCAPOCICredentialsUsesInstancePrincipal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		credentials *CAPOCICredentials
		want        bool
	}{
		{
			name: "returns true for true",
			credentials: &CAPOCICredentials{
				UseInstancePrincipal: "true",
			},
			want: true,
		},
		{
			name: "returns false for false",
			credentials: &CAPOCICredentials{
				UseInstancePrincipal: "false",
			},
		},
		{
			name: "returns false for invalid flag",
			credentials: &CAPOCICredentials{
				UseInstancePrincipal: "invalid",
			},
		},
		{
			name:        "returns false for nil credentials",
			credentials: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.credentials.UsesInstancePrincipal(); got != tt.want {
				t.Fatalf("UsesInstancePrincipal() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestRequireCredentialValuesTrimsWhitespace(t *testing.T) {
	t.Parallel()

	err := requireCredentialValues("instance-principal auth", []credentialRequirement{
		{name: "OCI_REGION", value: "   "},
	})
	if err == nil {
		t.Fatal("requireCredentialValues() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "OCI_REGION") {
		t.Fatalf("requireCredentialValues() error = %q, want mention of OCI_REGION", err.Error())
	}
}

func TestAuthConfigData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		credentials *CAPOCICredentials
		want        map[string]string
	}{
		{
			name: "returns minimal fields for instance principal",
			credentials: &CAPOCICredentials{
				Region:               "us-phoenix-1",
				UseInstancePrincipal: "true",
			},
			want: map[string]string{
				authConfigRegionKey:               "us-phoenix-1",
				authConfigUseInstancePrincipalKey: "true",
			},
		},
		{
			name: "returns full key based fields without empty passphrase",
			credentials: &CAPOCICredentials{
				TenancyID:            "ocid1.tenancy.oc1..example",
				UserID:               "ocid1.user.oc1..example",
				Region:               "us-phoenix-1",
				Fingerprint:          "11:22:33",
				PrivateKey:           "-----BEGIN PRIVATE KEY-----",
				UseInstancePrincipal: "false",
			},
			want: map[string]string{
				authConfigTenancyKey:              "ocid1.tenancy.oc1..example",
				authConfigUserKey:                 "ocid1.user.oc1..example",
				authConfigRegionKey:               "us-phoenix-1",
				authConfigFingerprintKey:          "11:22:33",
				authConfigPrivateKeyKey:           "-----BEGIN PRIVATE KEY-----",
				authConfigUseInstancePrincipalKey: "false",
			},
		},
		{
			name: "includes passphrase when present",
			credentials: &CAPOCICredentials{
				TenancyID:            "ocid1.tenancy.oc1..example",
				UserID:               "ocid1.user.oc1..example",
				Region:               "us-phoenix-1",
				Fingerprint:          "11:22:33",
				PrivateKey:           "-----BEGIN PRIVATE KEY-----",
				UseInstancePrincipal: "false",
				Passphrase:           "topsecret",
			},
			want: map[string]string{
				authConfigTenancyKey:              "ocid1.tenancy.oc1..example",
				authConfigUserKey:                 "ocid1.user.oc1..example",
				authConfigRegionKey:               "us-phoenix-1",
				authConfigFingerprintKey:          "11:22:33",
				authConfigPrivateKeyKey:           "-----BEGIN PRIVATE KEY-----",
				authConfigUseInstancePrincipalKey: "false",
				authConfigPassphraseKey:           "topsecret",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := authConfigData(tt.credentials)
			if len(got) != len(tt.want) {
				t.Fatalf("authConfigData() len = %d, want %d", len(got), len(tt.want))
			}
			for key, wantValue := range tt.want {
				gotValue, ok := got[key]
				if !ok {
					t.Fatalf("authConfigData() missing key %q", key)
				}
				if string(gotValue) != wantValue {
					t.Fatalf("authConfigData()[%q] = %q, want %q", key, string(gotValue), wantValue)
				}
			}
		})
	}
}

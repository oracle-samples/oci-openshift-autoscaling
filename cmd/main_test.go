/*
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package main

import (
	"context"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/oci-capi-operator/internal/components/capoci"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestValidateOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		options Options
		wantErr string
	}{
		{
			name: "accepts valid key based credentials",
			options: Options{
				CAPOCICredentials: capoci.CAPOCICredentials{
					TenancyID:            "ocid1.tenancy.oc1..example",
					UserID:               "ocid1.user.oc1..example",
					Region:               "us-phoenix-1",
					Fingerprint:          "11:22:33",
					PrivateKey:           "-----BEGIN PRIVATE KEY-----",
					UseInstancePrincipal: "false",
				},
				CSRApprovalConfig: CSRApprovalConfig{ClusterName: "test-cluster"},
			},
		},
		{
			name: "accepts valid instance principal credentials",
			options: Options{
				CAPOCICredentials: capoci.CAPOCICredentials{
					Region:               "us-phoenix-1",
					UseInstancePrincipal: "true",
				},
				CSRApprovalConfig: CSRApprovalConfig{ClusterName: "test-cluster"},
			},
		},
		{
			name: "rejects invalid startup credentials",
			options: Options{
				CAPOCICredentials: capoci.CAPOCICredentials{
					UseInstancePrincipal: "false",
				},
				CSRApprovalConfig: CSRApprovalConfig{ClusterName: "test-cluster"},
			},
			wantErr: "key-based auth requires OCI_TENANCY_ID, OCI_USER_ID, OCI_REGION, OCI_CREDENTIALS_FINGERPRINT, OCI_CREDENTIALS_KEY",
		},
		{
			name: "rejects missing CSR cluster scope",
			options: Options{
				CAPOCICredentials: capoci.CAPOCICredentials{
					Region:               "us-phoenix-1",
					UseInstancePrincipal: "true",
				},
			},
			wantErr: "CSR approval requires CSR_CLUSTER_NAME or CLUSTER_NAME",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateOptions(tt.options)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateOptions() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateOptions() error = nil, want %q", tt.wantErr)
			}
			if err.Error() != tt.wantErr {
				t.Fatalf("validateOptions() error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestResolveCSRApprovalConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      CSRApprovalConfig
		envCluster  string
		infraName   string
		wantCluster string
		wantMachine string
		wantErr     bool
	}{
		{
			name:        "keeps explicit CSR cluster name",
			config:      CSRApprovalConfig{ClusterName: "configured-cluster"},
			infraName:   "infra-cluster",
			wantCluster: "configured-cluster",
			wantMachine: defaultCSRMachineNamespace,
		},
		{
			name:        "keeps explicit CSR machine namespace",
			config:      CSRApprovalConfig{ClusterName: "configured-cluster", MachineNamespace: "custom-machine-ns"},
			infraName:   "infra-cluster",
			wantCluster: "configured-cluster",
			wantMachine: "custom-machine-ns",
		},
		{
			name:        "falls back to legacy CLUSTER_NAME environment variable",
			envCluster:  "env-cluster",
			infraName:   "infra-cluster",
			wantCluster: "env-cluster",
			wantMachine: defaultCSRMachineNamespace,
		},
		{
			name:        "discovers cluster name from OpenShift infrastructure",
			infraName:   "infra-cluster",
			wantCluster: "infra-cluster",
			wantMachine: defaultCSRMachineNamespace,
		},
		{
			name:    "rejects empty resolved cluster scope",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CLUSTER_NAME", tt.envCluster)
			scheme := runtime.NewScheme()
			if err := configv1.Install(scheme); err != nil {
				t.Fatalf("install config scheme: %v", err)
			}

			builder := fake.NewClientBuilder().WithScheme(scheme)
			if tt.infraName != "" {
				builder = builder.WithObjects(&configv1.Infrastructure{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
					Status: configv1.InfrastructureStatus{
						InfrastructureName: tt.infraName,
					},
				})
			}

			config := tt.config
			err := resolveCSRApprovalConfig(context.Background(), builder.Build(), &config)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveCSRApprovalConfig() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveCSRApprovalConfig() unexpected error: %v", err)
			}
			if config.ClusterName != tt.wantCluster {
				t.Fatalf("ClusterName = %q, want %q", config.ClusterName, tt.wantCluster)
			}
			if config.MachineNamespace != tt.wantMachine {
				t.Fatalf("MachineNamespace = %q, want %q", config.MachineNamespace, tt.wantMachine)
			}
		})
	}
}

/*
Copyright (c) 2025, 2026, Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package utils

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/coreos/ignition/v2/config/v3_2/types"
	configv1 "github.com/openshift/api/config/v1"
	oconfig "github.com/openshift/oci-capi-operator/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestSecondaryNICScriptInvariants(t *testing.T) {
	script := strings.TrimSpace(oconfig.SecondaryNICScript)
	if script == "" {
		t.Fatalf("SecondaryNICScript must not be empty")
	}
	if !strings.HasPrefix(script, "#!/usr/bin/env bash") {
		t.Fatalf("SecondaryNICScript must start with #!/usr/bin/env bash")
	}

	requiredSnippets := []string{
		"nmcli",
		"curl",
		"169.254.169.254",
		"KUBELET_NODE_IP",
		"/etc/NetworkManager/system-connections",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(script, snippet) {
			t.Fatalf("SecondaryNICScript missing required snippet: %s", snippet)
		}
	}

	forbiddenSnippets := []string{
		"ISCSI_HANDOFF_SNAPSHOT",
		"ISCSI_HANDOFF_MONITOR",
		"iscsi-diag:",
		"snapshot_network_state",
		"nmcli device reapply",
		"convert_primary_to_static_metadata_only",
		"ipv4.method manual",
		"ip route replace \"${target_ip}/32\"",
		"No active connection found for primary interface",
		"creating static connection",
		"nmcli connection up \"${if_name}\"",
		"harden_secondary_connection",
		"ipv4.route-metric 200",
		"iscsiadm -m session -P 3",
	}
	for _, snippet := range forbiddenSnippets {
		if strings.Contains(script, snippet) {
			t.Fatalf("SecondaryNICScript must not include unwanted experiment snippet: %s", snippet)
		}
	}
}

func TestISCSIProtectPrimaryRouteScriptInvariants(t *testing.T) {
	script := strings.TrimSpace(iscsiProtectPrimaryRouteScript)
	if script == "" {
		t.Fatalf("iscsiProtectPrimaryRouteScript must not be empty")
	}
	requiredSnippets := []string{
		`/sys/firmware/ibft/target0/ip-addr`,
		`169.254.0.2`,
		`/dev/console`,
		`/dev/kmsg`,
		`<4>%s\n`,
		`iscsi-protect:`,
		`logger -t iscsi-protect`,
		`ISCSI_PROTECT_ITERATIONS:-1800`,
		`UNIT_START pid=$$`,
		`WATCHDOG_START`,
		`START label=`,
		`/sys/firmware/ibft/ethernet`,
		`/etc/NetworkManager/conf.d/99-ibft.conf`,
		`unmanaged-devices=mac:%s`,
		`NM_UNMANAGED primary=`,
		`subnet-mask`,
		`nmguard(){`,
		`restore(){`,
		`RESTORE_IP label=`,
		`ip addr replace "$ip/$pre" dev "$dev"`,
		`ip link set dev "$dev" up`,
		`ip route replace "$t/32"`,
		`src "$primary_src" metric 1`,
		`PRIMARY_ADDR_MISSING`,
		`ROUTE_GET`,
		`DONE`,
		`WATCHDOG_DONE`,
		`exit 0`,
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(script, snippet) {
			t.Fatalf("iscsiProtectPrimaryRouteScript missing required snippet: %s", snippet)
		}
	}

	forbiddenSnippets := []string{
		"nmcli",
		"curl",
		"network-online.target",
		"connection up",
		"device reapply",
		"systemctl restart NetworkManager",
		"seq 1 90",
	}
	for _, snippet := range forbiddenSnippets {
		if strings.Contains(script, snippet) {
			t.Fatalf("iscsiProtectPrimaryRouteScript must not include high-risk snippet: %s", snippet)
		}
	}
}

func TestGeneratedIgnitionFitsOCIUserDataBudget(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := configv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add configv1 scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 scheme: %v", err)
	}

	machineConfigCA := "-----BEGIN CERTIFICATE-----\n" + strings.Repeat("A", 2048) + "\n-----END CERTIFICATE-----\n"
	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(
			&configv1.Infrastructure{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Status: configv1.InfrastructureStatus{
					APIServerInternalURL: "https://api-int.example.com:6443",
				},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "machine-config-server-tls",
					Namespace: "openshift-machine-config-operator",
				},
				Data: map[string][]byte{
					"tls.crt": []byte(machineConfigCA),
				},
			},
		).
		Build()

	ignitionConfig, err := GenerateIgnitionConfig(context.Background(), client)
	if err != nil {
		t.Fatalf("failed to generate ignition config: %v", err)
	}

	const (
		ociUserDataLimitBytes         = 32000
		ociMetadataAccountingOverhead = 64
	)
	metadataBytes := base64.StdEncoding.EncodedLen(len([]byte(ignitionConfig))) + ociMetadataAccountingOverhead
	if metadataBytes > ociUserDataLimitBytes {
		t.Fatalf("generated ignition metadata is %d bytes, above OCI user_data limit %d; raw ignition bytes=%d", metadataBytes, ociUserDataLimitBytes, len([]byte(ignitionConfig)))
	}
	t.Logf("generated ignition raw=%d metadata-accounted=%d OCI user_data limit=%d", len([]byte(ignitionConfig)), metadataBytes, ociUserDataLimitBytes)
}

func TestGeneratedIgnitionExcludesRemovedDiagnostics(t *testing.T) {
	ignitionConfig := generateTestIgnitionConfig(t)
	forbiddenSnippets := []string{
		"oci-dns-diagnostic",
		"oci-boot-marker",
		"machine-config-daemon-pull.service.d",
		"NetworkManager.service.d/10-oci-boot-marker.conf",
	}
	for _, snippet := range forbiddenSnippets {
		if strings.Contains(ignitionConfig, snippet) {
			t.Fatalf("generated ignition must not include removed diagnostic artifact: %s", snippet)
		}
	}
}

func generateTestIgnitionConfig(t *testing.T) string {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := configv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add configv1 scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 scheme: %v", err)
	}
	machineConfigCA := "-----BEGIN CERTIFICATE-----\n" + strings.Repeat("A", 2048) + "\n-----END CERTIFICATE-----\n"
	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(
			&configv1.Infrastructure{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Status: configv1.InfrastructureStatus{
					APIServerInternalURL: "https://api-int.example.com:6443",
				},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "machine-config-server-tls",
					Namespace: "openshift-machine-config-operator",
				},
				Data: map[string][]byte{
					"tls.crt": []byte(machineConfigCA),
				},
			},
		).
		Build()
	ignitionConfig, err := GenerateIgnitionConfig(context.Background(), client)
	if err != nil {
		t.Fatalf("failed to generate ignition config: %v", err)
	}
	return ignitionConfig
}

func TestISCSIProtectServiceUnitInvariants(t *testing.T) {
	unit := strings.TrimSpace(iscsiProtectServiceUnit)
	if unit == "" {
		t.Fatalf("iscsiProtectServiceUnit must not be empty")
	}

	requiredSnippets := []string{
		"[Unit]",
		"DefaultDependencies=no",
		"Wants=network-pre.target",
		"Before=network-pre.target NetworkManager.service sysinit.target",
		"ConditionPathExists=/sys/firmware/ibft",
		"[Service]",
		"Type=simple",
		"ExecStart=/etc/oci-iscsi/iscsi-protect-primary-route-watchdog.sh",
		"StandardOutput=journal+console",
		"StandardError=journal+console",
		"[Install]",
		"WantedBy=sysinit.target",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(unit, snippet) {
			t.Fatalf("iscsiProtectServiceUnit missing required snippet: %s", snippet)
		}
	}

	forbiddenSnippets := []string{
		"nmcli",
		"curl",
		"network-online.target",
		"connection up",
		"device reapply",
		"systemctl restart NetworkManager",
		"ExecStartPost=",
		"systemd-network-generator.service",
	}
	for _, snippet := range forbiddenSnippets {
		if strings.Contains(unit, snippet) {
			t.Fatalf("iscsiProtectServiceUnit must not include high-risk snippet: %s", snippet)
		}
	}
}

func TestISCSIProtectServiceLinkTarget(t *testing.T) {
	if iscsiProtectServiceLinkTarget != "../iscsi-protect-primary-route.service" {
		t.Fatalf("iscsi protect service link target must use relative sysinit target link")
	}
}

func TestSetHostnameOCIScriptInvariants(t *testing.T) {
	script := strings.TrimSpace(setHostnameOCIScript)
	if script == "" {
		t.Fatalf("setHostnameOCIScript must not be empty")
	}
	requiredSnippets := []string{
		`Authorization: Bearer Oracle`,
		`/opc/v2/instance/`,
		`hostnamectl set-hostname`,
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(script, snippet) {
			t.Fatalf("setHostnameOCIScript missing required snippet: %s", snippet)
		}
	}
}

func TestSetHostnameServiceRunsBeforeKubelet(t *testing.T) {
	unitContents := setHostnameOCIServiceUnit
	if !strings.Contains(unitContents, "Before=kubelet.service") {
		t.Fatalf("set-hostname unit must run before kubelet")
	}
	requiredSnippets := []string{
		"ExecStart=/usr/local/bin/set-hostname-oci.sh",
		"WantedBy=multi-user.target",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(unitContents, snippet) {
			t.Fatalf("set-hostname unit missing required snippet: %s", snippet)
		}
	}
	forbiddenSnippets := []string{
		"oci-boot-marker.sh",
		"ExecStartPre=",
		"ExecStartPost=",
	}
	for _, snippet := range forbiddenSnippets {
		if strings.Contains(unitContents, snippet) {
			t.Fatalf("set-hostname unit must not include removed diagnostic hook: %s", snippet)
		}
	}
}

func TestOCISecondaryNICServiceRunsBeforeKubelet(t *testing.T) {
	unitContents := ociSecondaryNICServiceUnit
	if !strings.Contains(unitContents, "Before=NetworkManager-wait-online.service network-online.target ovs-configuration.service kubelet.service") {
		t.Fatalf("oci-secondary-nic unit must run before kubelet")
	}
	requiredSnippets := []string{
		"After=NetworkManager.service",
		"Before=NetworkManager-wait-online.service network-online.target ovs-configuration.service kubelet.service",
		"ExecStart=/usr/local/bin/iscsi-oci-configure-secondary-nic.sh",
		"WantedBy=network-online.target multi-user.target",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(unitContents, snippet) {
			t.Fatalf("oci-secondary-nic unit missing required snippet: %s", snippet)
		}
	}
	forbiddenSnippets := []string{
		"oci-boot-marker.sh",
		"ExecStartPre=",
		"ExecStartPost=",
	}
	for _, snippet := range forbiddenSnippets {
		if strings.Contains(unitContents, snippet) {
			t.Fatalf("oci-secondary-nic unit must not include removed diagnostic hook: %s", snippet)
		}
	}
}

func TestSetHostnameScriptEmbedsInIgnition(t *testing.T) {
	scriptB64 := base64.StdEncoding.EncodeToString([]byte(setHostnameOCIScript))
	rawConfig := fmt.Sprintf(`{"storage":{"files":[{"path":"/usr/local/bin/set-hostname-oci.sh","contents":{"source":"data:text/plain;charset=utf-8;base64,%s"}}]}}`, scriptB64)

	var cfg types.Config
	if err := json.Unmarshal([]byte(rawConfig), &cfg); err != nil {
		t.Fatalf("failed to unmarshal ignition fragment: %v", err)
	}

	source := cfg.Storage.Files[0].Contents.Source
	if source == nil {
		t.Fatalf("embedded hostname script must include a source")
	}

	encodedScript := strings.TrimPrefix(*source, "data:text/plain;charset=utf-8;base64,")
	decodedScript, err := base64.StdEncoding.DecodeString(encodedScript)
	if err != nil {
		t.Fatalf("failed to decode embedded hostname script: %v", err)
	}

	if !strings.Contains(string(decodedScript), "Authorization: Bearer Oracle") {
		t.Fatalf("embedded hostname script must preserve OCI IMDS auth header")
	}
}

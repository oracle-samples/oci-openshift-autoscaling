/*
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
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

func TestOCIDNSDiagnosticScriptInvariants(t *testing.T) {
	script := strings.TrimSpace(ociDNSDiagnosticScript)
	if script == "" {
		t.Fatalf("ociDNSDiagnosticScript must not be empty")
	}
	requiredSnippets := []string{
		`oci-dns-diagnostic:`,
		`/dev/console`,
		`/dev/kmsg`,
		`logger -t oci-dns-diagnostic`,
		`ns="169.254.169.254"`,
		`/etc/resolv.conf`,
		`RESULT label=${label} resolv_conf_issue=`,
		`FIX_START label=${label}`,
		`FIX_DONE label=${label} path=/etc/resolv.conf nameserver=${ns}`,
		`nameserver %s\n`,
		`mv -f "${tmp}" /etc/resolv.conf`,
		`timeout 5 getent hosts quay.io`,
		`RESULT label=${label} resolve_quay_rc=`,
		`DONE label=${label}`,
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(script, snippet) {
			t.Fatalf("ociDNSDiagnosticScript missing required snippet: %s", snippet)
		}
	}

	forbiddenSnippets := []string{
		"log_cmd",
		"ip -br addr show",
		"ip route show",
		"ip route get 169.254.0.2",
		"systemctl list-jobs",
		"FILE_READLINK",
		"connection show --active",
		"nmcli",
		"/run/systemd/resolve/stub-resolv.conf",
		"/run/systemd/resolve/resolv.conf",
		"/run/NetworkManager/resolv.conf",
		"/run/NetworkManager/no-stub-resolv.conf",
		"nmcli connection modify",
		"nmcli connection up",
		"ip route replace",
		"systemctl restart",
		"tee /etc/resolv.conf",
		"nmcli device reapply",
	}
	for _, snippet := range forbiddenSnippets {
		if strings.Contains(script, snippet) {
			t.Fatalf("ociDNSDiagnosticScript must not include high-risk snippet: %s", snippet)
		}
	}
}

func TestOCIDNSDiagnosticBeforeMCDPullUnitInvariants(t *testing.T) {
	unit := strings.TrimSpace(ociDNSDiagnosticBeforeMCDPullServiceUnit)
	if unit == "" {
		t.Fatalf("ociDNSDiagnosticBeforeMCDPullServiceUnit must not be empty")
	}
	requiredSnippets := []string{
		"Description=OCI DNS diagnostic and resolv.conf protection before MCD firstboot image pull",
		"DefaultDependencies=no",
		"Wants=NetworkManager-wait-online.service",
		"After=NetworkManager.service NetworkManager-wait-online.service",
		"Before=machine-config-daemon-pull.service",
		"ConditionPathExists=/run/ostree-booted",
		"ConditionPathExists=/etc/ignition-machine-config-encapsulated.json",
		"ExecStart=/etc/oci-iscsi/oci-dns-diagnostic.sh before-mcd-pull",
		"TimeoutStartSec=30",
		"StandardOutput=journal+console",
		"StandardError=journal+console",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(unit, snippet) {
			t.Fatalf("ociDNSDiagnosticBeforeMCDPullServiceUnit missing required snippet: %s", snippet)
		}
	}
}

func TestOCIBootMarkerScriptInvariants(t *testing.T) {
	script := strings.TrimSpace(ociBootMarkerScript)
	if script == "" {
		t.Fatalf("ociBootMarkerScript must not be empty")
	}
	requiredSnippets := []string{
		`oci-boot-marker:`,
		`/dev/console`,
		`/dev/kmsg`,
		`logger -t oci-boot-marker`,
		`MARK label=`,
		`ADDR label=`,
		`ROUTE_GET label=`,
		`169.254.0.2`,
		`DEFAULT_ROUTE label=`,
		`SYSTEMD_STATE label=`,
		`exit 0`,
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(script, snippet) {
			t.Fatalf("ociBootMarkerScript missing required snippet: %s", snippet)
		}
	}

	forbiddenSnippets := []string{
		"nmcli",
		"curl",
		"connection up",
		"device reapply",
		"systemctl restart",
		"ip route replace",
	}
	for _, snippet := range forbiddenSnippets {
		if strings.Contains(script, snippet) {
			t.Fatalf("ociBootMarkerScript must not include mutating network snippet: %s", snippet)
		}
	}
}

func TestOCIBootMarkerUnitInvariants(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		required []string
	}{
		{
			name:     "multi-user",
			contents: ociBootMarkerMultiUserServiceUnit,
			required: []string{
				"ExecStart=/etc/oci-iscsi/oci-boot-marker.sh multi-user-target",
				"StandardOutput=journal+console",
				"StandardError=journal+console",
				"WantedBy=multi-user.target",
			},
		},
		{
			name:     "firstboot-osupdate",
			contents: ociBootMarkerFirstbootOSUpdateServiceUnit,
			required: []string{
				"DefaultDependencies=no",
				"Before=machine-config-daemon-firstboot.service",
				"ExecStart=/etc/oci-iscsi/oci-boot-marker.sh firstboot-osupdate-target",
				"StandardOutput=journal+console",
				"StandardError=journal+console",
				"WantedBy=firstboot-osupdate.target",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, snippet := range tt.required {
				if !strings.Contains(tt.contents, snippet) {
					t.Fatalf("%s marker unit missing required snippet: %s", tt.name, snippet)
				}
			}
		})
	}
}

func TestMarkerDropinInvariants(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		required string
	}{
		{
			name:     "NetworkManager",
			contents: networkManagerMarkerDropin,
			required: "ExecStartPre=/etc/oci-iscsi/oci-boot-marker.sh NetworkManager-ExecStartPre",
		},
		{
			name:     "machine-config-daemon-firstboot",
			contents: machineConfigDaemonFirstbootMarkerDropin,
			required: "ExecStartPre=/etc/oci-iscsi/oci-boot-marker.sh machine-config-daemon-firstboot-ExecStartPre",
		},
		{
			name:     "machine-config-daemon-pull-dns-diagnostic",
			contents: machineConfigDaemonPullDNSDiagnosticDropin,
			required: "Wants=oci-dns-diagnostic-before-mcd-pull.service",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.contents, "[Service]") && !strings.Contains(tt.contents, "[Unit]") {
				t.Fatalf("%s marker drop-in must define [Service] or [Unit]", tt.name)
			}
			if !strings.Contains(tt.contents, tt.required) {
				t.Fatalf("%s marker drop-in missing required snippet: %s", tt.name, tt.required)
			}
		})
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
		"ExecStartPre=/etc/oci-iscsi/oci-boot-marker.sh set-hostname-oci-pre",
		"ExecStart=/usr/local/bin/set-hostname-oci.sh",
		"ExecStartPost=/etc/oci-iscsi/oci-boot-marker.sh set-hostname-oci-post",
		"WantedBy=multi-user.target",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(unitContents, snippet) {
			t.Fatalf("set-hostname unit missing required snippet: %s", snippet)
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
		"ExecStartPre=/etc/oci-iscsi/oci-boot-marker.sh oci-secondary-nic-pre",
		"ExecStart=/usr/local/bin/iscsi-oci-configure-secondary-nic.sh",
		"ExecStartPost=/etc/oci-iscsi/oci-boot-marker.sh oci-secondary-nic-post",
		"WantedBy=network-online.target multi-user.target",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(unitContents, snippet) {
			t.Fatalf("oci-secondary-nic unit missing required snippet: %s", snippet)
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

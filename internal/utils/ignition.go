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

	"github.com/coreos/ignition/v2/config/v3_2/types"
	"github.com/go-openapi/swag"
	oconfig "github.com/openshift/oci-capi-operator/config"

	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	ociUserDataLimitBytes         = 32000
	ociMetadataAccountingOverhead = 64
)

const setHostnameOCIScript = `#!/usr/bin/env bash
set -euo pipefail

# OCI IMDS v2 requires the Authorization header for metadata access.
hostname=$(curl --silent -H "Authorization: Bearer Oracle" -L http://169.254.169.254/opc/v2/instance/ | jq -r .displayName)

if [ -n "$hostname" ] && [ "$hostname" != "null" ]; then
    echo "Setting hostname to $hostname"
    hostnamectl set-hostname "$hostname"
else
    echo "Failed to get hostname from OCI metadata"
    exit 1
fi
`

const iscsiProtectPrimaryRouteScript = `#!/usr/bin/env bash
set -u

iters="${ISCSI_PROTECT_ITERATIONS:-1800}"
int="${ISCSI_PROTECT_INTERVAL:-1}"
l(){ m="iscsi-protect: $*"; printf '%s\n' "$m" || true; printf '%s\n' "$m" >/dev/console 2>/dev/null || true; printf '<4>%s\n' "$m" >/dev/kmsg 2>/dev/null || true; command -v logger >/dev/null 2>&1 && logger -t iscsi-protect -- "$*" || true; }
r(){ tr -d '[:space:]' < "$1/$2" 2>/dev/null || true; }
pfx(){ awk -F. '{for(i=1;i<=4;i++){v=$i+0;while(v){p+=v%2;v=int(v/2)}}print p}' <<< "${1:-}"; }
v4(){ [[ "${1:-}" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] && [ "$1" != 0.0.0.0 ]; }
src4(){ ip -4 -o addr show dev "$1" 2>/dev/null | awk '{split($4,a,"/");print a[1];exit}' || true; }
pick(){ local t="$1" e m n; for e in /sys/firmware/ibft/ethernet*; do [ -e "$e/mac" ] || continue; m="$(r "$e" mac | tr '[:upper:]' '[:lower:]')"; for n in /sys/class/net/*; do [ "$(cat "$n/address" 2>/dev/null || true)" = "$m" ] && { printf '%s %s\n' "$(basename "$n")" "$e"; return; }; done; done; ip route get "$t" 2>/dev/null | awk '{for(i=1;i<=NF;i++)if($i=="dev"){print $(i+1)" ";exit}}'; }
nmguard(){ local dev="$1" e="$2" mac f=/etc/NetworkManager/conf.d/99-ibft.conf; [ -n "$e" ] || return; [ -e "$f" ] && return; mac="$(r "$e" mac | tr '[:upper:]' '[:lower:]')"; [ -n "$mac" ] || return; mkdir -p /etc/NetworkManager/conf.d || return; { printf '[keyfile]\n'; printf 'unmanaged-devices=mac:%s\n' "$mac"; } > "$f" && l "NM_UNMANAGED primary=$dev mac=$mac"; }
restore(){ local lab="$1" dev="$2" e="$3" cur ip mask pre; cur="$(src4 "$dev")"; primary_src="$cur"; [ -n "$e" ] || return; ip="$(r "$e" ip-addr)"; v4 "$ip" || return; ip -4 -o addr show dev "$dev" 2>/dev/null | awk -v ip="$ip" '{split($4,a,"/");if(a[1]==ip)f=1}END{exit f?0:1}' && { primary_src="$ip"; return; }; mask="$(r "$e" subnet-mask)"; v4 "$mask" && pre="$(pfx "$mask" 2>/dev/null || true)" || pre=32; [ -n "$pre" ] || pre=32; l "RESTORE_IP label=$lab primary=$dev ip=$ip/$pre current=${cur:-none}"; ip addr replace "$ip/$pre" dev "$dev" || l "RESTORE_IP_FAILED label=$lab primary=$dev"; primary_src="$(src4 "$dev")"; }
once(){ local lab="${1:-single}" t dev e primary_src route verbose=0 n; t="$(cat /sys/firmware/ibft/target0/ip-addr 2>/dev/null || true)"; [ -n "$t" ] && [ "$t" != null ] || t=169.254.0.2; read -r dev e <<< "$(pick "$t")"; [ -n "$dev" ] || dev=ens300f0np0; nmguard "$dev" "$e"; case "$lab" in single|iter-1|iter-2|iter-3) verbose=1;; *) n="${lab#iter-}"; [[ "$n" =~ ^[0-9]+$ ]] && [ $((n%30)) -eq 0 ] && verbose=1;; esac; [ "$verbose" -eq 1 ] && l "START label=$lab target=$t primary=$dev"; if ip link show dev "$dev" >/dev/null 2>&1; then ip link set dev "$dev" up || l "LINK_UP_FAILED label=$lab primary=$dev"; restore "$lab" "$dev" "$e"; [ -n "$primary_src" ] && ip route replace "$t/32" dev "$dev" src "$primary_src" metric 1 || { l "PRIMARY_ADDR_MISSING label=$lab primary=$dev"; ip route replace "$t/32" dev "$dev" metric 1 || true; }; else l "MISSING_PRIMARY label=$lab primary=$dev"; fi; [ "$verbose" -eq 1 ] && { route="$(ip route get "$t" 2>&1 || true)"; l "ROUTE_GET label=$lab $route"; l "DONE label=$lab"; }; }
l "UNIT_START pid=$$ argv=${*:-none}"
l "WATCHDOG_START iterations=$iters interval=$int"
i=1
while [ "$i" -le "$iters" ]; do
  once "iter-$i"
  sleep "$int"
  i=$((i + 1))
done

l "WATCHDOG_DONE iterations=$iters"
exit 0
`

const iscsiProtectServiceUnit = `[Unit]
Description=OCI iSCSI primary route protect watchdog
DefaultDependencies=no
Wants=network-pre.target
Before=network-pre.target NetworkManager.service sysinit.target
ConditionPathExists=/sys/firmware/ibft

[Service]
Type=simple
ExecStart=/etc/oci-iscsi/iscsi-protect-primary-route-watchdog.sh
StandardOutput=journal+console
StandardError=journal+console

[Install]
WantedBy=sysinit.target
`

const setHostnameOCIServiceUnit = `[Unit]
Description=Set hostname from OCI metadata
After=network-online.target
Before=kubelet.service
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/local/bin/set-hostname-oci.sh

[Install]
WantedBy=multi-user.target
`

const ociSecondaryNICServiceUnit = `[Unit]
Description=Configure secondary OCI VNIC if present
After=NetworkManager.service
Before=NetworkManager-wait-online.service network-online.target ovs-configuration.service kubelet.service

[Service]
Type=oneshot
ExecStart=/usr/local/bin/iscsi-oci-configure-secondary-nic.sh

[Install]
WantedBy=network-online.target multi-user.target
`

const iscsiProtectServiceLinkTarget = "../iscsi-protect-primary-route.service"

func GenerateIgnitionConfig(ctx context.Context, client client.Client) (string, error) {
	logger := ctrllog.Log.WithName("ignition")
	apiServerInternalURL, err := GetClusterAPIServerInternalURL(ctx, client)
	if err != nil {
		return "", fmt.Errorf("failed to get cluster API server internal URL: %w", err)
	}
	apiServerInternalURL = strings.TrimPrefix(apiServerInternalURL, "https://")
	apiServerInternalURL, _, _ = strings.Cut(apiServerInternalURL, ":")
	logger.Info("Generating ignition config",
		"apiServerHost", apiServerInternalURL,
		"units", 3,
		"embeddedFiles", 3,
	)

	machineConfigCA, err := GetMachineConfigCA(ctx, client)
	if err != nil {
		return "", fmt.Errorf("failed to get machine config CA: %w", err)
	}
	machineConfigCAB64 := base64.StdEncoding.EncodeToString([]byte(machineConfigCA))

	setHostnameOCIScriptB64 := base64.StdEncoding.EncodeToString([]byte(setHostnameOCIScript))
	iscsiProtectPrimaryRouteScriptB64 := base64.StdEncoding.EncodeToString([]byte(iscsiProtectPrimaryRouteScript))
	ociSecondaryNICScriptB64 := base64.StdEncoding.EncodeToString([]byte(oconfig.SecondaryNICScript))

	ignitionConfig := &types.Config{
		Systemd: types.Systemd{
			Units: []types.Unit{
				{
					Name:     "iscsi-protect-primary-route.service",
					Contents: swag.String(iscsiProtectServiceUnit),
				},
				{
					Name:     "set-hostname-oci.service",
					Enabled:  swag.Bool(true),
					Contents: swag.String(setHostnameOCIServiceUnit),
				},
				{
					Name:     "oci-secondary-nic.service",
					Enabled:  swag.Bool(true),
					Contents: swag.String(ociSecondaryNICServiceUnit),
				},
			},
		},
		Storage: types.Storage{
			Directories: []types.Directory{
				{
					Node: types.Node{
						Path: "/etc/oci-iscsi",
					},
					DirectoryEmbedded1: types.DirectoryEmbedded1{
						Mode: swag.Int(493),
					},
				},
			},
			Files: []types.File{
				{
					Node: types.Node{
						Path: "/etc/oci-iscsi/iscsi-protect-primary-route-watchdog.sh",
					},
					FileEmbedded1: types.FileEmbedded1{
						Contents: types.Resource{
							Source: swag.String(fmt.Sprintf("data:text/plain;charset=utf-8;base64,%s", iscsiProtectPrimaryRouteScriptB64)),
						},
						Mode: swag.Int(493),
					},
				},
				{
					Node: types.Node{
						Path: "/usr/local/bin/set-hostname-oci.sh",
					},
					FileEmbedded1: types.FileEmbedded1{
						Contents: types.Resource{
							Source: swag.String(fmt.Sprintf("data:text/plain;charset=utf-8;base64,%s", setHostnameOCIScriptB64)),
						},
						Mode: swag.Int(493),
					},
				},
				{
					Node: types.Node{
						Path: "/usr/local/bin/iscsi-oci-configure-secondary-nic.sh",
					},
					FileEmbedded1: types.FileEmbedded1{
						Contents: types.Resource{
							Source: swag.String(fmt.Sprintf("data:text/plain;charset=utf-8;base64,%s", ociSecondaryNICScriptB64)),
						},
						Mode: swag.Int(493),
					},
				},
			},
			Links: []types.Link{
				{
					Node: types.Node{
						Path: "/etc/systemd/system/sysinit.target.wants/iscsi-protect-primary-route.service",
					},
					LinkEmbedded1: types.LinkEmbedded1{
						Target: iscsiProtectServiceLinkTarget,
					},
				},
			},
		},
		Ignition: types.Ignition{
			Version: "3.2.0",
			Security: types.Security{
				TLS: types.TLS{
					CertificateAuthorities: []types.Resource{
						{
							Source: swag.String(fmt.Sprintf("data:text/plain;charset=utf-8;base64,%s", machineConfigCAB64)),
						},
					},
				},
			},
			Config: types.IgnitionConfig{
				Merge: []types.Resource{
					{
						Source: swag.String(fmt.Sprintf("https://%s:22623/config/worker", apiServerInternalURL)),
					},
				},
			},
		},
	}

	ignitionConfigBytes, err := json.Marshal(ignitionConfig)
	if err != nil {
		return "", fmt.Errorf("failed to marshal ignition config: %w", err)
	}
	ociMetadataBytes := base64.StdEncoding.EncodedLen(len(ignitionConfigBytes)) + ociMetadataAccountingOverhead
	if ociMetadataBytes > ociUserDataLimitBytes {
		return "", fmt.Errorf("generated ignition metadata is %d bytes, above OCI user_data limit %d; raw ignition bytes=%d", ociMetadataBytes, ociUserDataLimitBytes, len(ignitionConfigBytes))
	}
	logger.Info("Generated ignition config", "bytes", len(ignitionConfigBytes), "ociMetadataBytes", ociMetadataBytes, "ociUserDataLimitBytes", ociUserDataLimitBytes)
	return string(ignitionConfigBytes), nil
}

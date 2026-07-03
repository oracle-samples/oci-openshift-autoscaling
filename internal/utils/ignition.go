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

const ociBootMarkerScript = `#!/usr/bin/env bash
set -u

function log {
  local msg="oci-boot-marker: $*"
  printf '%s\n' "${msg}" || true
  printf '%s\n' "${msg}" >&2 || true
  printf '%s\n' "${msg}" > /dev/console 2>/dev/null || true
  printf '<4>%s\n' "${msg}" > /dev/kmsg 2>/dev/null || true
  if command -v logger >/dev/null 2>&1; then
    logger -t oci-boot-marker -- "$*" || true
  fi
}

label="${1:-unknown}"
target_ip="169.254.0.2"
uptime="$(cut -d' ' -f1 /proc/uptime 2>/dev/null || true)"
boot_id="$(cat /proc/sys/kernel/random/boot_id 2>/dev/null || true)"

log "MARK label=${label} pid=$$ uptime=${uptime} boot_id=${boot_id}"

if command -v ip >/dev/null 2>&1; then
  log "ADDR label=${label} $(ip -br addr show 2>&1 || true)"
  log "ROUTE_GET label=${label} $(ip route get "${target_ip}" 2>&1 || true)"
  log "DEFAULT_ROUTE label=${label} $(ip route show default 2>&1 || true)"
fi

if command -v systemctl >/dev/null 2>&1; then
  log "SYSTEMD_STATE label=${label} default=$(systemctl get-default 2>&1 || true) failed=$(systemctl --failed --no-legend 2>&1 | wc -l | tr -d ' ' || true)"
fi

exit 0
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

const ociDNSDiagnosticScript = `#!/usr/bin/env bash
set -u

label="${1:-unknown}"
ns="169.254.169.254"
l(){ m="oci-dns-diagnostic: $*"; printf '%s\n' "$m" || true; printf '%s\n' "$m" >/dev/console 2>/dev/null || true; printf '<4>%s\n' "$m" >/dev/kmsg 2>/dev/null || true; command -v logger >/dev/null 2>&1 && logger -t oci-dns-diagnostic -- "$*" || true; }
l "START label=${label} pid=$$"
issue=none
[ -L /etc/resolv.conf ] && [ ! -e /etc/resolv.conf ] && issue=broken_symlink
[ "${issue}" = none ] && [ ! -e /etc/resolv.conf ] && issue=missing
[ "${issue}" = none ] && ! grep -Eq '^[[:space:]]*nameserver[[:space:]]+' /etc/resolv.conf 2>/dev/null && issue=no_nameserver
if [ "${issue}" = none ] && grep -Eq '^[[:space:]]*nameserver[[:space:]]+(::1|127\.0\.0\.1|127\.0\.0\.53)' /etc/resolv.conf 2>/dev/null && ! systemctl is-active --quiet systemd-resolved.service 2>/dev/null; then
  issue=loopback_resolver_inactive
fi

l "RESULT label=${label} resolv_conf_issue=${issue}"
if [ "${issue}" != none ]; then
  l "FIX_START label=${label} reason=${issue} nameserver=${ns}"
  tmp="/etc/resolv.conf.oci.$$"
  [ -L /etc/resolv.conf ] && rm -f /etc/resolv.conf
  { printf 'nameserver %s\n' "${ns}"; printf 'options timeout:1 attempts:3\n'; } > "${tmp}" 2>/dev/null && chmod 0644 "${tmp}" 2>/dev/null && mv -f "${tmp}" /etc/resolv.conf
  rc=$?
  [ "${rc}" -eq 0 ] && l "FIX_DONE label=${label} path=/etc/resolv.conf nameserver=${ns}" || l "FIX_FAILED label=${label} reason=${issue} rc=${rc}"
  rm -f "${tmp}" 2>/dev/null || true
else
  rc=0
fi

tmp="/tmp/oci-dns-quay.$$"
rc=0
timeout 5 getent hosts quay.io > "${tmp}" 2>&1 || rc=$?
rm -f "${tmp}" 2>/dev/null || true
l "RESULT label=${label} resolve_quay_rc=${rc}"
l "DONE label=${label}"
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

const ociDNSDiagnosticBeforeMCDPullServiceUnit = `[Unit]
Description=OCI DNS diagnostic and resolv.conf protection before MCD firstboot image pull
DefaultDependencies=no
Wants=NetworkManager-wait-online.service
After=NetworkManager.service NetworkManager-wait-online.service
Before=machine-config-daemon-pull.service
ConditionPathExists=/run/ostree-booted
ConditionPathExists=/etc/ignition-machine-config-encapsulated.json

[Service]
Type=oneshot
ExecStart=/etc/oci-iscsi/oci-dns-diagnostic.sh before-mcd-pull
TimeoutStartSec=30
StandardOutput=journal+console
StandardError=journal+console
`

const setHostnameOCIServiceUnit = `[Unit]
Description=Set hostname from OCI metadata
After=network-online.target
Before=kubelet.service
Wants=network-online.target

[Service]
Type=oneshot
ExecStartPre=/etc/oci-iscsi/oci-boot-marker.sh set-hostname-oci-pre
ExecStart=/usr/local/bin/set-hostname-oci.sh
ExecStartPost=/etc/oci-iscsi/oci-boot-marker.sh set-hostname-oci-post

[Install]
WantedBy=multi-user.target
`

const ociSecondaryNICServiceUnit = `[Unit]
Description=Configure secondary OCI VNIC if present
After=NetworkManager.service
Before=NetworkManager-wait-online.service network-online.target ovs-configuration.service kubelet.service

[Service]
Type=oneshot
ExecStartPre=/etc/oci-iscsi/oci-boot-marker.sh oci-secondary-nic-pre
ExecStart=/usr/local/bin/iscsi-oci-configure-secondary-nic.sh
ExecStartPost=/etc/oci-iscsi/oci-boot-marker.sh oci-secondary-nic-post

[Install]
WantedBy=network-online.target multi-user.target
`

const ociBootMarkerMultiUserServiceUnit = `[Unit]
Description=OCI boot marker for multi-user target transaction
After=basic.target
ConditionPathExists=/etc/oci-iscsi/oci-boot-marker.sh

[Service]
Type=oneshot
ExecStart=/etc/oci-iscsi/oci-boot-marker.sh multi-user-target
StandardOutput=journal+console
StandardError=journal+console

[Install]
WantedBy=multi-user.target
`

const ociBootMarkerFirstbootOSUpdateServiceUnit = `[Unit]
Description=OCI boot marker for firstboot osupdate target transaction
DefaultDependencies=no
Before=machine-config-daemon-firstboot.service
ConditionPathExists=/etc/oci-iscsi/oci-boot-marker.sh

[Service]
Type=oneshot
ExecStart=/etc/oci-iscsi/oci-boot-marker.sh firstboot-osupdate-target
StandardOutput=journal+console
StandardError=journal+console

[Install]
WantedBy=firstboot-osupdate.target
`

const networkManagerMarkerDropin = `[Service]
ExecStartPre=/etc/oci-iscsi/oci-boot-marker.sh NetworkManager-ExecStartPre
`

const machineConfigDaemonFirstbootMarkerDropin = `[Service]
ExecStartPre=/etc/oci-iscsi/oci-boot-marker.sh machine-config-daemon-firstboot-ExecStartPre
`

const machineConfigDaemonPullDNSDiagnosticDropin = `[Unit]
Wants=oci-dns-diagnostic-before-mcd-pull.service
After=oci-dns-diagnostic-before-mcd-pull.service
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
		"units", 5,
		"embeddedFiles", 7,
	)

	machineConfigCA, err := GetMachineConfigCA(ctx, client)
	if err != nil {
		return "", fmt.Errorf("failed to get machine config CA: %w", err)
	}
	machineConfigCAB64 := base64.StdEncoding.EncodeToString([]byte(machineConfigCA))

	setHostnameOCIScriptB64 := base64.StdEncoding.EncodeToString([]byte(setHostnameOCIScript))
	ociBootMarkerScriptB64 := base64.StdEncoding.EncodeToString([]byte(ociBootMarkerScript))
	iscsiProtectPrimaryRouteScriptB64 := base64.StdEncoding.EncodeToString([]byte(iscsiProtectPrimaryRouteScript))
	ociDNSDiagnosticScriptB64 := base64.StdEncoding.EncodeToString([]byte(ociDNSDiagnosticScript))
	ociSecondaryNICScriptB64 := base64.StdEncoding.EncodeToString([]byte(oconfig.SecondaryNICScript))

	ignitionConfig := &types.Config{
		Systemd: types.Systemd{
			Units: []types.Unit{
				{
					Name:     "iscsi-protect-primary-route.service",
					Contents: swag.String(iscsiProtectServiceUnit),
				},
				{
					Name:     "oci-dns-diagnostic-before-mcd-pull.service",
					Contents: swag.String(ociDNSDiagnosticBeforeMCDPullServiceUnit),
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
				{
					Name:     "oci-boot-marker-multi-user.service",
					Enabled:  swag.Bool(true),
					Contents: swag.String(ociBootMarkerMultiUserServiceUnit),
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
				{
					Node: types.Node{
						Path: "/etc/systemd/system/NetworkManager.service.d",
					},
					DirectoryEmbedded1: types.DirectoryEmbedded1{
						Mode: swag.Int(493),
					},
				},
				{
					Node: types.Node{
						Path: "/etc/systemd/system/machine-config-daemon-pull.service.d",
					},
					DirectoryEmbedded1: types.DirectoryEmbedded1{
						Mode: swag.Int(493),
					},
				},
			},
			Files: []types.File{
				{
					Node: types.Node{
						Path: "/etc/oci-iscsi/oci-boot-marker.sh",
					},
					FileEmbedded1: types.FileEmbedded1{
						Contents: types.Resource{
							Source: swag.String(fmt.Sprintf("data:text/plain;charset=utf-8;base64,%s", ociBootMarkerScriptB64)),
						},
						Mode: swag.Int(493),
					},
				},
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
						Path: "/etc/oci-iscsi/oci-dns-diagnostic.sh",
					},
					FileEmbedded1: types.FileEmbedded1{
						Contents: types.Resource{
							Source: swag.String(fmt.Sprintf("data:text/plain;charset=utf-8;base64,%s", ociDNSDiagnosticScriptB64)),
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
				{
					Node: types.Node{
						Path: "/etc/systemd/system/NetworkManager.service.d/10-oci-boot-marker.conf",
					},
					FileEmbedded1: types.FileEmbedded1{
						Contents: types.Resource{
							Source: swag.String(fmt.Sprintf("data:text/plain;charset=utf-8;base64,%s", base64.StdEncoding.EncodeToString([]byte(networkManagerMarkerDropin)))),
						},
						Mode: swag.Int(420),
					},
				},
				{
					Node: types.Node{
						Path: "/etc/systemd/system/machine-config-daemon-pull.service.d/10-oci-dns-diagnostic.conf",
					},
					FileEmbedded1: types.FileEmbedded1{
						Contents: types.Resource{
							Source: swag.String(fmt.Sprintf("data:text/plain;charset=utf-8;base64,%s", base64.StdEncoding.EncodeToString([]byte(machineConfigDaemonPullDNSDiagnosticDropin)))),
						},
						Mode: swag.Int(420),
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

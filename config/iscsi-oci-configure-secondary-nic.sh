#!/usr/bin/env bash
# Copyright (c) 2025, 2026 Oracle and/or its affiliates.
# Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.


set -e
set -x

if [ ! -d "/sys/firmware/ibft" ]
then
  echo "No IBFT configuration found. Skipping."
  exit 0
fi

MTU=9000
NODEIP_CONF="/etc/systemd/system/kubelet.service.d/30-oci-nodeip.conf"

function get_if_name_from_mac_address {
  mac_address="${1}"
  ip -json link | jq --raw-output --arg mac_address "${mac_address}" '. | map(select(.address==($mac_address|ascii_downcase))) | .[0].ifname'
}

# Get the primary interface used for the default route
function get_primary_if_name {
  ip -json route show default | jq --raw-output '.[] | select(.dst == "default") | .dev' | head -n1
}

# Extract the prefix from the primary interface name
function extract_prefix {
  primary_if_name="${1}"
  if [[ "${primary_if_name}" =~ ^([a-zA-Z]+) ]]; then
    echo "${BASH_REMATCH[1]}"
  else
    echo ""
  fi
}

# Get the secondary interface name based on the dynamic prefix
function get_secondary_if_name_by_prefix {
  prefix="${1}"
  all_interfaces=$(ip -json link show | jq --raw-output '.[].ifname')

  for ifname in ${all_interfaces}; do
    if [[ "${ifname}" == "${prefix}"* && "${ifname}" != "${primary_if_name}" ]]; then
      echo "${ifname}"
      return 0
    fi
  done

  echo "No secondary interface found with the prefix ${prefix}."
  return 1
}

# Ensure OCI DNS is configured on a given network connection
function set_oci_dns {
  local conn_name="$1"
  dns_value=$(nmcli -g ipv4.dns connection show "$conn_name")
  if [[ -z "$dns_value" || "$dns_value" == "--" ]]; then
    echo "Setting OCI DNS for $conn_name"
    nmcli connection modify "$conn_name" ipv4.dns "169.254.169.254"
    nmcli connection modify "$conn_name" ipv4.ignore-auto-dns no
    nmcli connection up "$conn_name"
  fi
}

#Fetch the VNICS data
vnics=$(curl --silent -H "Authorization: Bearer Oracle" -L http://169.254.169.254/opc/v2/vnics/)
secondary_if_mac_address=$(jq -r '.[1].macAddr' <<< "${vnics}")
secondary_if_ip_address=$(jq -r '.[1].privateIp' <<< "${vnics}")
secondary_if_vlan_tag=$(jq -r '.[1].vlanTag' <<< "${vnics}")
secondary_if_default_gateway=$(jq -r '.[1].virtualRouterIp' <<< "${vnics}")
secondary_if_subnet=$(jq -r '.[1].subnetCidrBlock' <<< "${vnics}")
secondary_if_subnet_size=$(cut -f 2 -d '/' <<< "${secondary_if_subnet}")
secondary_if_name=$(get_if_name_from_mac_address "${secondary_if_mac_address}")

# If the interface name is null or empty, fallback to using the IP address
if [[ -z "${secondary_if_name}" || "${secondary_if_name}" == "null" ]]; then
  echo "MAC address not found, falling back to prefix lookup method."
  primary_if_name=$(get_primary_if_name)
  prefix=$(extract_prefix "${primary_if_name}")
  secondary_if_name=$(get_secondary_if_name_by_prefix "${prefix}")
  if [ "${secondary_if_vlan_tag}" -ne 0 ]; then
    echo "VLAN Tag is not 0, network configuration needs to be modified at the VLAN level"
    if [ ! -f "/etc/NetworkManager/system-connections/${secondary_if_name}.nmconnection" ]; then
        nmcli connection add type vlan con-name "${secondary_if_name}.${secondary_if_vlan_tag}" ifname "${secondary_if_name}.${secondary_if_vlan_tag}" dev "${secondary_if_name}" id "${secondary_if_vlan_tag}" ip4 "${secondary_if_ip_address}/${secondary_if_subnet_size}" gw4 "${secondary_if_default_gateway}"
        nmcli connection modify "${secondary_if_name}.${secondary_if_vlan_tag}" 802-3-ethernet.cloned-mac-address "${secondary_if_mac_address}"
        nmcli connection modify "${secondary_if_name}.${secondary_if_vlan_tag}" 802-3-ethernet.mtu ${MTU}
        nmcli connection modify "${secondary_if_name}.${secondary_if_vlan_tag}" ipv4.route-metric 0
        nmcli connection modify "${secondary_if_name}.${secondary_if_vlan_tag}" connection.autoconnect true
        nmcli connection reload
        nmcli connection up "${secondary_if_name}.${secondary_if_vlan_tag}"
        for uuid in $(nmcli -t -f UUID,DEVICE connection show | grep ':--' | cut -d: -f1); do
          nmcli connection delete "$uuid"
        done
        set_oci_dns "${secondary_if_name}.${secondary_if_vlan_tag}"
    fi
  else
    if [ ! -f "/etc/NetworkManager/system-connections/${secondary_if_name}.nmconnection" ]; then
      nmcli connection add con-name "${secondary_if_name}" ifname "${secondary_if_name}" type ethernet ip4 "${secondary_if_ip_address}/${secondary_if_subnet_size}" gw4 "${secondary_if_default_gateway}"
      nmcli connection modify "${secondary_if_name}" ethernet.mtu ${MTU}
      nmcli connection modify "${secondary_if_name}" ipv4.route-metric 0
      nmcli connection modify "${secondary_if_name}" connection.autoconnect true
      nmcli connection reload
      nmcli connection up "${secondary_if_name}"
      set_oci_dns "${secondary_if_name}"
    fi
  fi
else
  if [ ! -f "/etc/NetworkManager/system-connections/${secondary_if_name}.nmconnection" ]; then
    nmcli connection add con-name "${secondary_if_name}" ifname "${secondary_if_name}" type ethernet ip4 "${secondary_if_ip_address}/${secondary_if_subnet_size}" gw4 "${secondary_if_default_gateway}"
    nmcli connection modify "${secondary_if_name}" ethernet.mtu ${MTU}
    nmcli connection modify "${secondary_if_name}" ipv4.route-metric 0
    nmcli connection modify "${secondary_if_name}" connection.autoconnect true
    nmcli connection reload
    nmcli connection up "${secondary_if_name}"
    set_oci_dns "${secondary_if_name}"
  fi
fi

# Don't proceed further if we run in the discovery environment
if [ -n "${ASSISTED_INSTALLER_DISCOVERY_ENV}" ]
then
  exit 0
fi

# Force the kubelet to use the IP of the secondary VNIC for as node IP
if [ ! -f "${NODEIP_CONF}" ]
then
  cat << EOF > "${NODEIP_CONF}"
[Service]
Environment="KUBELET_NODE_IP=${secondary_if_ip_address}"
EOF
fi

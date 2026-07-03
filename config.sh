#!/bin/bash
# Copyright (c) 2025, 2026 Oracle and/or its affiliates.
# Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.


# Set to true to deploy CAPOCI in instance-principal mode. This only changes
# the operator's runtime auth path; the local oci CLI still needs to work so
# this script can discover compartment, VCN, subnet, and load balancer IDs.
use_instance_principal=false

oci_region="$(grep -E "^region=" ~/.oci/config | cut -d = -f 2)"
export OCI_REGION="\"${oci_region}\""
export OCI_USE_INSTANCE_PRINCIPAL="\"${use_instance_principal}\""

if [[ "${use_instance_principal}" == "true" ]]; then
  export OCI_TENANCY_ID="\"\""
  export OCI_USER_ID="\"\""
  export OCI_CREDENTIALS_FINGERPRINT=""
  export OCI_CREDENTIALS_KEY=""
  export fingerprint=""
  export privateKey=""
  export passphrase=""
else
  export OCI_TENANCY_ID="\"$(grep -E "^tenancy=" ~/.oci/config | cut -d = -f 2)\""
  export OCI_USER_ID="\"$(grep -E "^user=" ~/.oci/config | cut -d = -f 2)\""
  export OCI_CREDENTIALS_FINGERPRINT="$(grep -E "^fingerprint=" ~/.oci/config | cut -d = -f 2)"
  export OCI_CREDENTIALS_KEY="$(grep -E "^key_file=" ~/.oci/config | cut -d = -f 2)"
  export privateKey="$(base64 < "${OCI_CREDENTIALS_KEY}" | tr -d '\n')"
  export fingerprint="$(echo "${OCI_CREDENTIALS_FINGERPRINT}" | tr -d '\n' | base64)"
  export passphrase="$(grep -E "^passphrase=" ~/.oci/config | cut -d = -f 2 | tr -d '\n' | base64)"
fi

# Required Config Variables
compartment_name=
image_name=
compartment_id="$(oci iam compartment list --all --compartment-id-in-subtree true --access-level ACCESSIBLE --raw-output --query "data[?name=='$compartment_name'].id | [0]")"
nsg_name=
compute_nsg_name=
ocp_subnet_name=
bare_metal_subnet_name=
cluster_name=$(oc get infrastructure cluster -ojsonpath='{.status.infrastructureName}')
oci_cluster_name=$(echo "$cluster_name" | rev | cut -d - -f 2- | rev)
vcn_id="$(oci network vcn list --compartment-id "$compartment_id" --display-name "$oci_cluster_name" | jq -r '.data[0].id')"

# Default Autoscaling Config Variables
export AUTOSCALER_MIN_NODES="\"0\""
export AUTOSCALER_MAX_NODES="\"5\""
export AUTOSCALER_CPUS="\"6\""
export AUTOSCALER_MEMORY="\"16\""
export AUTOSCALER_SHAPE="\"VM.Standard.E5.Flex\""
export AUTOSCALER_DEFINED_TAGS_NAMESPACE=

# OCI compartment
export COMPARTMENT_ID="\"$compartment_id\""

# IMAGE_ID is the OCID of the RHCOS image to use for new worker nodes added to the cluster
export IMAGE_ID="\"$(oci compute image list --compartment-id "$compartment_id" --display-name "$image_name" | jq -r '.data[0].id')\""

# OCI networking setup 
export VCN_ID="\"$vcn_id\""
export OCP_SUBNET_ID="\"$(oci network subnet list --compartment-id "$compartment_id" --vcn-id "$vcn_id" --display-name "$ocp_subnet_name" | jq -r '.data[0].id')\""
export OCP_SUBNET_NAME="\"${ocp_subnet_name}\""
export NSG_ID="\"$(oci network nsg list --compartment-id "$compartment_id" --vcn-id "$vcn_id" --display-name "$nsg_name" | jq -r '.data[0].id')\""
export COMPUTE_NSG_NAME="\"${compute_nsg_name}\""
export API_LB_ID="\"$(oci lb load-balancer list --compartment-id $compartment_id --display-name ${oci_cluster_name}-openshift_apps_lb | jq -r '.data[].id')\""
export API_LB_IP="\"$(oci lb load-balancer list --compartment-id $compartment_id --display-name ${oci_cluster_name}-openshift_apps_lb | jq -r '.data[]."ip-addresses"[] | select(."is-public" == true) | ."ip-address"')\""
export SVC_CIDR="\"$(oc get network.config.openshift.io cluster -o jsonpath='{.spec.serviceNetwork[*]}')\""
export CLUSTER_CIDR="\"$(oc get network.config.openshift.io cluster -o jsonpath='{.spec.clusterNetwork[*].cidr}')\""

# Optional: Secondary network attachments (leave empty unless a distinct secondary subnet/NSG is required)
export BARE_METAL_SUBNET_ID="\"$(oci network subnet list --compartment-id "$compartment_id" --vcn-id "$vcn_id" --display-name "$bare_metal_subnet_name" | jq -r '.data[0].id')\""
export BARE_METAL_SUBNET_NAME="\"$bare_metal_subnet_name\""

echo "All Variables set"

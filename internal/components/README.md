<!--
Copyright (c) 2025, 2026, Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
-->

# Components

The components this operator will deploy.

Each component encapsulates all of the subcomponents required
to deploy it.

## Namespace contract

The controller defaults provider controllers, managed Cluster API resources,
cluster-autoscaler resources, and cluster-autoscaler autodiscovery to the
operator pod namespace. Deployment-level namespace settings may override these
defaults; the CR does not expose namespace overrides.

## Autoscaler

Cluster autoscaler deployment uses the helm library.

Additional components outside of the helm chart include:
- Namespace CR for the operator namespace
- ClusterRole CR
- ClusterRoleBinding CR

## CAPI

CAPI deployment uses the clusterctl library.

Additional components outside of the default clusterctl generation include:
- Separate SecurityContextConstraints (SCC) CRs for CAPI and CAPOCI
- Namespace CR for the operator namespace
- ClusterRoleBinding CR
- Secret for the service account

## CAPOCI

CAPOCI deployment uses the clusterctl library.

Additional components outside of the default clusterctl generation include:
- Namespace CR for the configured CAPOCI provider namespace
- Secret CR (auth config secret to authenticate to OCI)

## CRDs

The CRDs are deployed in an init container for this operator.
It ensures all of the CRDs exist in the cluster ahead of the operator.

Includes all of the CAPI, CAPOCI CRDs.

## Enable Autoscaler

The CAPI CRs required to enable autoscaling include:
- Cluster CR
- OCICluster CR
- MachineDeployment CR
- OCIMachineTemplate CR

<!--
Copyright (c) 2025, 2026, Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
-->

# OCIClusterAutoscaler API

This document is an API reference only. Installation, verification, scale testing, cleanup, and troubleshooting are owned by the `oracle-quickstart/oci-openshift` Terraform stack documentation:
https://github.com/oracle-quickstart/oci-openshift/blob/main/docs/AUTOSCALER.md

Do not use this repository's `config/` manifests as the production deployment workflow.

## Autoscaling

- `spec.autoscaling.minNodes`: minimum node count. Must be `>= 0`.
- `spec.autoscaling.maxNodes`: maximum node count. Must be `>= 0`.
- `spec.autoscaling.shape`: OCI compute shape for autoscaling nodes.
- `spec.autoscaling.shapeConfig`: optional flexible shape configuration. When `cpus` or `memory` is set, the value must be greater than `0`.
- `spec.autoscaling.imageId`: custom RHCOS image OCID used by autoscaling workers.
- `spec.autoscaling.poolIdentifier`: optional lowercase suffix for autoscaler node pool resources. Maximum length is 5 characters. The generated node pool name must stay within 51 characters. Immutable after creation.

## CAPI

- `spec.capi.clusterName`: optional CAPI cluster name override. Must be a valid Kubernetes object name. If set, it must fit the 51-character generated node pool name limit together with `spec.autoscaling.poolIdentifier`. Immutable after creation.

## Cluster Autoscaler

- `spec.clusterAutoscaler.repositoryURL`: Helm chart repository URL.
- `spec.clusterAutoscaler.name`: cluster-autoscaler Deployment name. Must be a valid Kubernetes object name. Immutable after creation.
- `spec.clusterAutoscaler.serviceAccountName`: service account name used by cluster-autoscaler. Must be a valid Kubernetes object name.
- `spec.clusterAutoscaler.cloudProvider`: Helm chart cloud provider value.
- `spec.clusterAutoscaler.createRBAC`: whether Helm should create RBAC resources.
- `spec.clusterAutoscaler.createServiceAccount`: whether Helm should create the service account.
- `spec.clusterAutoscaler.version`: Helm chart version.

## Namespaces

Provider, managed resource, cluster-autoscaler install, and autodiscovery namespaces are deployment-level operator configuration, not CR fields.

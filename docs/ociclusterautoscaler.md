<!--
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
-->

# OCIClusterAutoscaler CR

The OCIClusterAutoscaler CR has spec fields that can modify the default values used by this operator in deploying/enabling autoscaling.
The fields are not required on creation. The spec fields will take precedence over the default values if set.

## Spec fields

The OCIClusterAutoscaler spec contains three main sections:

### Autoscaling Configuration
Required field that configures the autoscaling behavior:

- `autoscaling`: Configuration for the autoscaling group
  - `minNodes`: Minimum number of nodes in the autoscaling group (minimum: 0)
  - `maxNodes`: Maximum number of nodes in the autoscaling group
  - `shape`: OCI compute shape for autoscaling nodes
  - `poolIdentifier`: Optional lowercase identifier appended to autoscaler node pool resource names, up to 5 characters. This field is immutable after creation.
  - `shapeConfig`: Optional flexible shape configuration
    - `cpus`: Number of OCPUs
    - `memory`: Amount of memory in GB
  - `imageId`: OCID of the custom RHCOS image for deploying new nodes during autoscaling

### CAPI Configuration
Optional configuration for Cluster API resources:

- `capi`: CAPI deployment configuration
  - `namespace`: Namespace where CAPI resources will be created
  - `clusterName`: Name of the CAPI cluster

### Cluster Autoscaler Configuration
Optional configuration for the cluster-autoscaler deployment:

- `clusterAutoscaler`: Cluster autoscaler deployment configuration
  - `repositoryURL`: URL for the helm chart of the cluster-autoscaler
  - `name`: Name of the cluster-autoscaler deployment
  - `namespace`: Namespace where the cluster-autoscaler deployment will be installed
  - `serviceAccountName`: Name of the service account the cluster-autoscaler deployment will use
  - `cloudProvider`: Cloud provider to use for the helm chart
  - `createRBAC`: Whether to create the RBAC resources from the helm chart (boolean)
  - `createServiceAccount`: Whether to create the service account from the helm chart (boolean)
  - `version`: Helm chart version of the cluster-autoscaler to install

Example:
```yaml
apiVersion: capi.oci.oracle.com/v1alpha1
kind: OCIClusterAutoscaler
metadata:
  name: example-autoscaler
spec:
  autoscaling:
    minNodes: 1
    maxNodes: 5
    shape: "VM.Standard.E4.Flex"
    poolIdentifier: "vm01"
    shapeConfig:
      cpus: 2
      memory: 16
    imageId: "ocid1.image.oc1.example..."
  capi:
    namespace: "oci-openshift-autoscaling-operator"
    clusterName: "example-cluster"
  clusterAutoscaler:
    name: "cluster-autoscaler"
    namespace: "kube-system"
    version: "9.29.0"
    createRBAC: true
    createServiceAccount: true
```

## Update min/max nodes

Change the autoscaling bounds by patching the existing `OCIClusterAutoscaler` CR. For example, scale the autoscaling group down to zero:

```sh
oc patch ociclusterautoscaler.capi.openshift.io -n oci-openshift-autoscaling-operator ociclusterautoscaler \
  --type=merge \
  -p '{"spec":{"autoscaling":{"minNodes":0,"maxNodes":0}}}'
oc get machinedeployment.cluster.x-k8s.io -n oci-openshift-autoscaling-operator
```

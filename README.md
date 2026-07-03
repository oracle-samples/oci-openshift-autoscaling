<!--
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v1.0 as shown at https://oss.oracle.com/licenses/upl/.
-->

# OCI OpenShift Autoscaling Operator

The OCI OpenShift Autoscaling Operator automates the deployment and management of Cluster API, Cluster API Provider OCI, and Kubernetes cluster-autoscaler components for Red Hat OpenShift clusters running on Oracle Cloud Infrastructure.

This project is intended for developers and operators who need to enable autoscaling for OpenShift worker nodes on OCI. It provides an operator, Kubernetes manifests, sample custom resources, and helper scripts for preparing the OCI and OpenShift configuration that the autoscaling stack requires.

## Getting Started

### Prerequisites

- Red Hat OpenShift cluster running on OCI.
- Local `kubeconfig` file for the OpenShift cluster.
- Oracle Cloud Infrastructure CLI (`oci`) installed and configured.
- OpenShift CLI (`oc`) installed.
- Go 1.25 or later.
- Docker Desktop or another Docker-compatible container runtime with Buildx support.
- `jq`, `curl`, `gzip`, and `openssl` available on your local machine.
- OCI permissions to inspect compartments, virtual cloud networks, subnets, network security groups, load balancers, and custom images.

See the Red Hat documentation for creating an OpenShift cluster on OCI:
https://docs.redhat.com/en/documentation/openshift_container_platform/4.19/html/installing_on_oci/installing-oci-assisted-installer#installing-oci-about-assisted-installer_installing-oci-assisted-installer

### Install the Source

Clone the repository:

```sh
git clone https://github.com/oracle-samples/oci-openshift-autoscaling.git
cd oci-openshift-autoscaling
```

Set your OpenShift cluster credentials:

```sh
export KUBECONFIG=/path/to/kubeconfig
```

Verify local access:

```sh
oc whoami
oci iam region list
```

### Configure OCI Access

The `config.sh` helper uses the local `oci` CLI to discover OCI resource identifiers for the target cluster. The CLI must be authenticated before you source the helper script.

This operator supports two Cluster API Provider OCI authentication modes:

- Instance principal mode: CAPOCI uses instance principal authentication from the cluster-side workload.
- API key mode: CAPOCI uses explicit OCI user credentials from the local OCI CLI configuration.

For instance principal mode, set this value in `config.sh`:

```sh
use_instance_principal=true
```

For API key mode, keep this value in `config.sh`:

```sh
use_instance_principal=false
```

If you need to configure OCI API key authentication, follow the Oracle Cloud Infrastructure API signing key instructions:
https://docs.oracle.com/en-us/iaas/Content/API/Concepts/apisigningkey.htm

### Prepare the Red Hat CoreOS Image

Autoscaled worker nodes require a Red Hat CoreOS image that matches the OpenShift cluster version.

For example, for OpenShift 4.19:

```sh
curl -LO https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.19/4.19.0/rhcos-4.19.0-x86_64-openstack.x86_64.qcow2.gz
gzip -d rhcos-4.19.0-x86_64-openstack.x86_64.qcow2.gz
```

Upload the image to OCI and create a custom image. See the OCI custom image documentation:
https://docs.oracle.com/en-us/iaas/Content/Compute/Tasks/importingcustomimagelinux.htm

Record the custom image name. You will use it in `config.sh`.

For bare metal worker nodes, prepare a Red Hat CoreOS image with the required iSCSI configuration before uploading it to Object Storage and creating the custom image.

### Configure the Deployment

Edit `config.sh` and set the required values:

```sh
compartment_name=
image_name=
nsg_name=
compute_nsg_name=
ocp_subnet_name=
bare_metal_subnet_name=
```

Source the configuration:

```sh
source config.sh
```

The helper exports the environment variables used to render the deployment manifests.

### Build

Build the manager binary:

```sh
make build
```

Build a container image:

```sh
export IMG=<registry>/<repository>/oci-openshift-autoscaling-operator:<tag>
make build-image
```

Build and push a multi-architecture image:

```sh
export IMG=<registry>/<repository>/oci-openshift-autoscaling-operator:<tag>
make buildx
```

### Deploy

Deploy the operator:

```sh
export IMG=<registry>/<repository>/oci-openshift-autoscaling-operator:<tag>
make deploy
```

By default, the operator and autoscaling stack run in the `oci-openshift-autoscaling-operator` namespace. `cert-manager` remains in the upstream `cert-manager` namespace.

Create the sample `OCIClusterAutoscaler` resource:

```sh
make apply
```

Verify the deployments and Cluster API resources:

```sh
oc get pods -n oci-openshift-autoscaling-operator
oc get cluster -n oci-openshift-autoscaling-operator
oc get ocicluster -n oci-openshift-autoscaling-operator
oc get machinedeployment -n oci-openshift-autoscaling-operator
oc get ocimachinetemplate -n oci-openshift-autoscaling-operator
```

### Clean Up

Remove only the OCI OpenShift Autoscaling Operator resources:

```sh
make cleanup-autoscaler
```

Remove the operator, Cluster API resources, CAPOCI resources, and cluster-autoscaler resources:

```sh
make cleanup-autoscaler-full
```

The full cleanup target is destructive. It preserves `cert-manager` by default. To remove `cert-manager` resources installed by provider bootstrap, opt in explicitly:

```sh
make cleanup-autoscaler-full AUTOSCALER_DELETE_CERT_MANAGER=true
```

## Documentation

Developer-oriented documentation is maintained in this repository:

- [`OCIClusterAutoscaler` custom resource fields](docs/ociclusterautoscaler.md)
- Component implementation notes in `internal/components/README.md`
- Operator packaging and deployment assets under `config/`

Product documentation for Oracle products and services is published on https://docs.oracle.com.

## Examples

The repository includes a sample `OCIClusterAutoscaler` custom resource in `config/samples/capi_v1beta1_ociclusterautoscaler.yaml`.

Apply the sample:

```sh
make apply
```

Test autoscaling by creating workload demand that exceeds the current cluster capacity:

```sh
oc create deployment nginx --namespace default --image=docker.io/nginx:latest --replicas=0
oc set resources deployment -n default nginx --requests=memory=2Gi
oc scale deployment -n default nginx --replicas=20
```

Watch the autoscaling resources:

```sh
oc get machinedeployment -n oci-openshift-autoscaling-operator
oc get machineset -n oci-openshift-autoscaling-operator
oc get ocicluster -n oci-openshift-autoscaling-operator
oc get ocimachine -n oci-openshift-autoscaling-operator
oc get nodes
```

Scale the example workload back down:

```sh
oc scale deployment -n default nginx --replicas=0
```

## Help

Use GitHub issues in this repository for project questions, bug reports, and enhancement requests.

Do not use GitHub issues to report security vulnerabilities. Follow the responsible disclosure process in the [security guide](SECURITY.md).

For Oracle Cloud Infrastructure service issues, use your normal Oracle Support channel if you have an Oracle Support relationship.

## Contributing

This project welcomes contributions from the community. Before submitting a pull request, review the [contribution guide](CONTRIBUTING.md).

## Security

Please consult the [security guide](SECURITY.md) for the responsible security vulnerability disclosure process.

## License

Copyright (c) 2025, 2026 Oracle and/or its affiliates.

Released under the Universal Permissive License v1.0 as shown at https://oss.oracle.com/licenses/upl/.

<!--
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v1.0 as shown at https://oss.oracle.com/licenses/upl/.
-->

# OCI OpenShift Autoscaling Operator

The OCI OpenShift Autoscaling Operator automates the deployment and management of Cluster API, Cluster API Provider OCI, and Kubernetes cluster-autoscaler components for Red Hat OpenShift clusters running on Oracle Cloud Infrastructure.

This project is intended for developers and operators who need to enable autoscaling for OpenShift worker nodes on OCI. It provides an operator, Kubernetes manifests, sample custom resources, and helper scripts for preparing the OCI and OpenShift configuration that the autoscaling stack requires.

## Description

The OCI OpenShift Autoscaling Operator deploys all of the necessary components in order to enable autoscaling in an OCI OCP cluster:

- CAPI and CAPOCI installation management
- [Cluster-autoscaler](https://github.com/kubernetes/autoscaler/tree/master/cluster-autoscaler) deployment and configuration
- CAPI CRs to manage the OCI cluster
- Certificate approver for new nodes

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

### Deployment Source of Truth

The supported installation, verification, scale testing, cleanup, and troubleshooting flow is owned by the `oracle-quickstart/oci-openshift` Terraform stack documentation:
https://github.com/oracle-quickstart/oci-openshift/blob/main/docs/AUTOSCALER.md

Use that Terraform stack as the deployment source of truth. This repository keeps the operator source, CRD schema, generated manifest sources, and tests.

### Runtime Image

The current stable release image is:

```sh
docker pull ghcr.io/oracle-samples/openshift-oracle-capi-autoscaling:v1.0.0
```

The `latest` tag is maintained as a convenience alias to the current stable
release:

```sh
docker pull ghcr.io/oracle-samples/openshift-oracle-capi-autoscaling:latest
```

OLM bundles and release artifacts use the immutable release tag rather than
the `latest` alias. Customers subscribed to the `stable` channel receive the
newest published release automatically. Override `IMG` only for development
builds, and keep it fully qualified with a registry host.

### Development

Local prerequisites:

- Go `1.24+` matching `go.mod`
- Docker or another compatible container tool
- `oc` and a kubeconfig only when running cluster verification or cleanup helpers

Build locally:

```sh
make build
```

Build a local development image:

```sh
IMG=ghcr.io/<owner>/<repo>:<tag> make build-image
```

Push and release targets such as `make push-image`, `make buildx`, `make bundle-push`, and `make catalog-push` refuse unqualified image names and mutable `:latest` tags. Use `make publish-release-image` to publish the versioned multi-architecture image and then advance the `latest` alias to that release.

### Provider Version Compatibility

The operator pins provider component versions through `CAPI_VERSION` and `CAPOCI_VERSION` in the generated install manifests. The default provider versions are:

- CAPI `v1.14.0`
- CAPOCI `v0.24.0`

Treat provider version changes as an explicit upgrade:

- Set both provider versions through the Terraform stack path.
- Validate generated `clusterctl` components before rollout.
- Review Cluster API upgrade notes before crossing a major or minor boundary, because webhook contracts and generated management-cluster manifests can change.

## Documentation

The supported installation, verification, scale testing, cleanup, and troubleshooting flow is documented in the `oracle-quickstart/oci-openshift` Terraform stack documentation:
https://github.com/oracle-quickstart/oci-openshift/blob/main/docs/AUTOSCALER.md

Developer-oriented documentation is maintained in this repository:

- [`OCIClusterAutoscaler` custom resource fields](docs/ociclusterautoscaler.md)
- Component implementation notes in `internal/components/README.md`
- Operator packaging and deployment assets under `config/`

Product documentation for Oracle products and services is published on https://docs.oracle.com.

## Examples

The repository includes a sample `OCIClusterAutoscaler` custom resource in `config/samples/capi_v1beta1_ociclusterautoscaler.yaml`.

For production and customer validation flows, generate and apply the autoscaler manifest from the `oracle-quickstart/oci-openshift` Terraform stack. This repository's `config/` manifests are source and development assets.

After installation, check the autoscaler resource:

```sh
oc get ociclusterautoscaler -n oci-openshift-autoscaling-operator ociclusterautoscaler -o yaml
```

Update the autoscaler min/max node values:

```sh
oc patch ociclusterautoscaler.capi.openshift.io -n oci-openshift-autoscaling-operator ociclusterautoscaler \
  --type=merge \
  -p '{"spec":{"autoscaling":{"minNodes":1,"maxNodes":3}}}'
```

Watch the autoscaling resources:

```sh
oc get machinedeployments.cluster.x-k8s.io -n oci-openshift-autoscaling-operator
oc get machinesets.cluster.x-k8s.io -n oci-openshift-autoscaling-operator
oc get machines.cluster.x-k8s.io -n oci-openshift-autoscaling-operator -o wide
oc get ocimachines.infrastructure.cluster.x-k8s.io -n oci-openshift-autoscaling-operator -o wide
oc get nodes
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

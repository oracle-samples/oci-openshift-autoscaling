# Copyright (c) 2025, 2026 Oracle and/or its affiliates.
# Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.

# Build the manager binary
# Use the native build platform for the builder stage to avoid emulation-related
# crashes (seen as SIGSEGV in `go mod download` on Apple Silicon when targeting
# linux/amd64).
FROM --platform=$BUILDPLATFORM golang:1.25 AS builder
ARG BUILDPLATFORM
ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY cmd/ cmd/
COPY api/ api/
COPY config/ config/
COPY internal/ internal/

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build  -o manager ./cmd

ARG CAPOCI_VERSION
COPY clusterctl.yaml clusterctl.yaml
RUN sed -i.bak "s/CAPOCI_VERSION_PLACEHOLDER/${CAPOCI_VERSION}/g" clusterctl.yaml && rm -f clusterctl.yaml.bak

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
# The following is needed for clusterctl to generate component configs during runtime
COPY --from=builder --chown=65532:65532 /workspace/clusterctl.yaml /.config/clusterctl.yaml
COPY LICENSE.txt /licenses/LICENSE.txt
COPY THIRD_PARTY_LICENSES.txt /licenses/THIRD_PARTY_LICENSES.txt
USER 65532:65532

ENTRYPOINT ["/manager"]

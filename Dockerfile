# syntax=docker/dockerfile:1

#############      builder                                  #############
# Run the builder on the native build platform and cross-compile to the target
# arch (GOOS/GOARCH below). This avoids QEMU emulation of the whole Go toolchain.
# Go >= 1.26.10 is required: it hard-distrusts the compromised eMudhra intermediates
# (CVE-2026-111943, golang/go#82345) in crypto/x509, independent of the CA bundle.
FROM --platform=$BUILDPLATFORM golang:1.26.10@sha256:672c944cfd07599b5f1bb179869270cdc94c6254e41db4d431259693cb286b82 AS builder

ARG TARGETOS
ARG TARGETARCH
# `local` pins the build to the toolchain baked into the base image above instead of
# silently downloading whatever go.mod asks for mid-build.
ENV CGO_ENABLED=0 \
    GOTOOLCHAIN=local \
    GOOS=$TARGETOS \
    GOARCH=$TARGETARCH

WORKDIR /go/src/github.com/opendefensecloud/machine-controller-manager-provider-hcloud

# cache deps before copying the source so that source changes don't invalidate them
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# `go install` places cross-compiled binaries under $GOPATH/bin/$GOOS_$GOARCH;
# flatten that subdir so the COPY below works for both native and cross builds.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    make install && \
    if [ -d "/go/bin/${GOOS}_${GOARCH}" ]; then mv "/go/bin/${GOOS}_${GOARCH}/"* /go/bin/; fi

#############      base                                     #############
# debian13: its ca-certificates (20250419+deb13u1) drops the emSign ECC Root CA - G3 and
# AAA Certificate Services roots behind the compromised eMudhra intermediates;
# static-debian12 still ships both.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:d62067879de890a1def9da0e7766b7d20810a5e5555cef8506b752437d7a3e88 AS base
LABEL org.opencontainers.image.source="https://github.com/opendefensecloud/machine-controller-manager-provider-hcloud"

WORKDIR /

#############      machine-controller               #############
FROM base AS machine-controller
WORKDIR /

COPY --from=builder /go/bin/machine-controller /machine-controller
ENTRYPOINT ["/machine-controller"]

# Build the manager binary
FROM golang:1.26-alpine AS builder
ARG TARGETOS
ARG TARGETARCH

ENV STRONGBOX_VERSION=2.1.0

RUN os=$(go env GOOS) && arch=$(go env GOARCH) \
      && apk --no-cache add curl git \
      && curl -Ls https://github.com/uw-labs/strongbox/releases/download/v${STRONGBOX_VERSION}/strongbox_${STRONGBOX_VERSION}_${os}_${arch} \
           > /usr/local/bin/strongbox \
      && chmod +x /usr/local/bin/strongbox

# The policy engine shells out to conftest binary
ARG CONFTEST_VERSION=0.69.0
RUN os=$(go env GOOS) && arch=$(go env GOARCH) && case "$arch" in \
        amd64) arch=x86_64 ;; \
      esac \
      && curl -Ls https://github.com/open-policy-agent/conftest/releases/download/v${CONFTEST_VERSION}/conftest_${CONFTEST_VERSION}_Linux_${arch}.tar.gz \
           | tar xz -C /usr/local/bin conftest \
      && chmod +x /usr/local/bin/conftest

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
# GOTOOLCHAIN pins the exact toolchain declared by go.mod's `go` line, so the
# build isn't at the mercy of whatever patch version the base image ships.
RUN GOTOOLCHAIN=go$(awk '/^go /{print $2; exit}' go.mod) go mod download

# Copy the go source
COPY . .

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN \
 go test -v -cover ./... && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} \
    GOARCH=${TARGETARCH} \
    GOTOOLCHAIN=go$(awk '/^go /{print $2; exit}' go.mod) \
    go build -a -o tf-applier

FROM alpine:3.23

ENV USER_ID=65532

RUN adduser -S -H -u $USER_ID tf-applier \
      && apk --no-cache add ca-certificates git openssh-client

COPY --from=builder /usr/local/bin/strongbox /usr/local/bin/
COPY --from=builder /usr/local/bin/conftest /usr/local/bin/

WORKDIR /
COPY --from=builder /workspace/tf-applier .

ENV USER=tf-applier
# Setting HOME ensures git can write config file .gitconfig.
ENV HOME=/tmp

USER $USER_ID

ENTRYPOINT ["/tf-applier"]

# Build the manager binary
FROM golang:1.20-alpine as builder
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
COPY . .

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go generate ./... \
    && go test -v -cover ./... \
    && go build -a -o manager main.go

FROM alpine:3.17

# match git-sync user
ENV USER_ID=65533

RUN adduser -S -u $USER_ID tf-applier \
      && apk --no-cache add ca-certificates git openssh-client

WORKDIR /
COPY --from=builder /workspace/manager .

ENV USER=tf-applier

USER $USER_ID

ENTRYPOINT ["/manager"]

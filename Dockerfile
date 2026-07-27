# syntax=docker/dockerfile:1

# Multi-stage, from-source build used by CI (docker/build-push-action) and by
# humans (`docker build .`). The release pipeline instead packages GoReleaser's
# pre-built binary via Dockerfile.goreleaser — keep the two runtime stages in sync.

# ---- build ----
FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS build
WORKDIR /src

# Warm the module cache before copying the rest of the tree.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Provided by buildx; default so a plain `docker build` still works.
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=dev

# CGO off → a fully static binary that runs on distroless/static.
# NOTE: builds the root package (package main). If main.go lives under
# ./cmd/<name>, change the final build argument to match (and .goreleaser.yaml).
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/webhook .

# ---- runtime ----
FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/webhook /webhook
# distroless nonroot == uid/gid 65532. GROUP_NAME and the config are supplied by
# the Deployment (Helm chart); the webhook serves the extension apiserver.
USER 65532:65532
ENTRYPOINT ["/webhook"]

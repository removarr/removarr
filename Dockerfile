# syntax=docker/dockerfile:1

# The UI and the binary are built on the build machine's own architecture; the binary is
# cross-compiled for the target, so no stage needs emulation (spec section 11).

FROM --platform=$BUILDPLATFORM node:24-trixie-slim AS web
ENV COREPACK_ENABLE_DOWNLOAD_PROMPT=0
RUN corepack enable
# Corepack reads the pinned pnpm version from the package.json in the working directory,
# so every pnpm command runs inside web/.
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web ./
# Vite writes the build into ../internal/webui/dist.
RUN mkdir -p ../internal/webui/dist && pnpm build

FROM --platform=$BUILDPLATFORM golang:1.27-trixie AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd cmd
COPY internal internal
COPY --from=web /src/internal/webui/dist internal/webui/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
      go build -trimpath -ldflags='-s -w' -o /out/removarr ./cmd/removarr \
 && install -d -m 0700 /out/config

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/removarr /removarr
COPY --from=build --chown=65532:65532 /out/config /config
ENV REMOVARR_CONFIG_DIR=/config
EXPOSE 7272
VOLUME ["/config"]
USER 65532:65532
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --start-interval=2s --retries=3 CMD ["/removarr", "healthcheck"]
ENTRYPOINT ["/removarr"]

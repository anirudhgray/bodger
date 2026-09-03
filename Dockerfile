# syntax=docker/dockerfile:1
#
# Multi-stage build for bodger's single self-hosted artefact (ADR-0001,
# ADR-0007, docs/architecture.md §4): the web UI, the Go binary with it
# embedded, and a minimal runtime image with neither a Node nor a Go
# toolchain in it. Versions below are pinned to match .tool-versions, the
# same file asdf/mise and CI read - bump there and here together.

# ---- Web UI -----------------------------------------------------------
# Builds web/ the same way `make build-web` does locally: straight into
# internal/platform/webui/dist, which that package's go:embed directive
# matches (web/vite.config.ts's build.outDir is the relative path
# "../internal/platform/webui/dist", so the sibling internal/ directory
# has to exist at this same layout for the build to land in the right
# place).
FROM node:24.20.0-alpine AS web-build
WORKDIR /src
COPY web/package.json web/package-lock.json ./web/
RUN cd web && npm ci
COPY web/ ./web/
RUN mkdir -p internal/platform/webui/dist
RUN cd web && npm run build

# ---- Go binary ----------------------------------------------------------
# CGO_ENABLED=0 keeps modernc.org/sqlite's pure-Go driver static (ADR-0001,
# ADR-0007) - no C toolchain, no libc dependency in the final image.
# -tags timetzdata embeds the IANA time zone database (stdlib's
# time/tzdata) straight into the binary: the runtime stage below has no
# /usr/share/zoneinfo on disk at all, and BODGER_USER_TIMEZONE
# (docs/user-guide.md) needs one to resolve anything but UTC.
FROM golang:1.27.0-alpine AS go-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web-build /src/internal/platform/webui/dist ./internal/platform/webui/dist
RUN CGO_ENABLED=0 go build -trimpath -tags timetzdata -o /out/bodger ./cmd/bodger
# An empty /data, owned by distroless's numeric nonroot uid/gid (65532),
# for the runtime stage to copy in below. A Docker named volume mounted
# over a path takes on that path's ownership from the image the *first*
# time it's created (docs/user-guide.md's Docker section explains why this
# matters) - without this, the volume would come up root-owned and the
# nonroot process below couldn't write bodger.db into it.
RUN mkdir -p /data && chown 65532:65532 /data

# ---- Runtime --------------------------------------------------------------
# distroless static: no shell, no package manager, nothing but the
# binary and its data directory - the smallest image that can run a
# CGO_ENABLED=0 Go binary, and it runs as the nonroot user by default.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=go-build /out/bodger /usr/local/bin/bodger
# --chown is required here, not just on the earlier `chown` in go-build:
# a plain cross-stage COPY resets ownership to root:root regardless of
# what the source stage had, so without repeating it here the volume
# copy-up below would seed a root-owned /data the nonroot user can't
# write into.
COPY --chown=65532:65532 --from=go-build /data /data

# BODGER_DB_PATH points at the mount point docker-compose.yml's named
# volume covers, so the SQLite file (and its pre-migration backup,
# ADR-0007) survive a container recreate. BODGER_HTTP_BIND_ADDR is left
# at its application default (127.0.0.1:8080, ADR-0006) - see
# docs/user-guide.md's Docker section for why that has to stay loopback
# and how the port is actually exposed today.
ENV BODGER_DB_PATH=/data/bodger.db

VOLUME ["/data"]
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/bodger"]
CMD ["serve"]

# syntax=docker/dockerfile:1.7

# ── 1. Frontend ───────────────────────────────────────────────────────────
FROM --platform=$BUILDPLATFORM node:24-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ── 2. Backend (pure Go, cross-compiled; no CGO) ──────────────────────────
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS server
ARG TARGETOS TARGETARCH VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
COPY web/embed.go ./web/embed.go
COPY --from=web /web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/labdash ./cmd/labdash

# ── 3. Nuclei (web vulnerability checks), checksum-verified ───────────────
FROM --platform=$BUILDPLATFORM alpine:3.22 AS nuclei
ARG TARGETARCH NUCLEI_VERSION=3.11.1
WORKDIR /dl
RUN apk add --no-cache curl unzip \
 && base="https://github.com/projectdiscovery/nuclei/releases/download/v${NUCLEI_VERSION}" \
 && file="nuclei_${NUCLEI_VERSION}_linux_${TARGETARCH}.zip" \
 && curl -fsSLO "$base/$file" \
 && curl -fsSL "$base/nuclei_${NUCLEI_VERSION}_checksums.txt" | grep " ${file}\$" | sha256sum -c - \
 && unzip -q "$file" nuclei && chmod 0755 nuclei

# ── 4. Runtime ────────────────────────────────────────────────────────────
FROM alpine:3.22
# nmap: network scanning · iputils: ICMP ping · tzdata: correct local times
RUN apk add --no-cache ca-certificates tzdata nmap nmap-scripts iputils \
 && mkdir -p /data
COPY --from=server /out/labdash /usr/local/bin/labdash
COPY --from=nuclei /dl/nuclei /usr/local/bin/nuclei
ENV LABDASH_DATA_DIR=/data PORT=8080
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
  CMD wget -qO- "http://127.0.0.1:${PORT}/healthz" >/dev/null || exit 1
ENTRYPOINT ["/usr/local/bin/labdash"]

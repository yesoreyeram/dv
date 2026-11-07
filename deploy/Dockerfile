# syntax=docker/dockerfile:1.7
# Multi-stage build: web assets -> static Go binary -> distroless non-root runtime.

FROM node:22-alpine AS web
WORKDIR /src
RUN corepack enable
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY web/package.json web/
COPY e2e/package.json e2e/
RUN pnpm install --frozen-lockfile --filter web...
COPY web/ web/
RUN pnpm --filter web run build

FROM golang:1.25-alpine AS go
WORKDIR /src
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
COPY go.mod go.sum ./
RUN go mod download
COPY api/ api/
COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/yesoreyeram/dv/internal/buildinfo.Version=${VERSION} -X github.com/yesoreyeram/dv/internal/buildinfo.Commit=${COMMIT} -X github.com/yesoreyeram/dv/internal/buildinfo.BuildDate=${BUILD_DATE}" \
    -o /out/dv ./cmd/dv

FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.source="https://github.com/yesoreyeram/dv" \
      org.opencontainers.image.description="dv — secure data visualization platform"
COPY --from=go /out/dv /usr/local/bin/dv
COPY --from=web /src/web/dist /usr/share/dv/web
ENV DV_HTTP_ADDR=":8080" \
    DV_WEB_DIR="/usr/share/dv/web" \
    DV_LOG_FORMAT="json"
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/dv"]

FROM golang:1.25.6-alpine AS build
RUN apk add --no-cache ca-certificates upx

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build-time metadata injected into internal/version via -ldflags.
# Defaults are safe fallbacks; CI / docker buildx should pass these as
# `--build-arg` to embed the real git tag, commit SHA, and Go version
# into the binary so they surface on the homepage footer.
ARG VERSION="0.0.9"
ARG BUILD_TIME="unknown"
ARG GIT_COMMIT="unknown"
ARG GO_VERSION="unknown"
ENV VERSION=${VERSION} \
    BUILD_TIME=${BUILD_TIME} \
    GIT_COMMIT=${GIT_COMMIT} \
    GO_VERSION=${GO_VERSION}

RUN go mod tidy \
    && CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -trimpath \
         -ldflags="-s -w \
                   -X github.com/bassista/go_spin/internal/version.Version=${VERSION} \
                   -X github.com/bassista/go_spin/internal/version.BuildTime=${BUILD_TIME} \
                   -X github.com/bassista/go_spin/internal/version.GitCommit=${GIT_COMMIT} \
                   -X github.com/bassista/go_spin/internal/version.GoVersion=${GO_VERSION} \
                   -extldflags '-static'" \
         -o /app/main ./cmd/server/main.go \
    && upx --best --lzma /app/main

#FROM gcr.io/distroless/static-debian11 AS prod
FROM alpine:3.20.1 AS prod
RUN apk add --no-cache su-exec

WORKDIR /app

COPY --from=build /app/main /app/main
COPY --from=build /app/ui /app/ui
COPY --from=build /app/config /app/config

# Remove unnecessary files from final image
RUN find /app -type f \( -name "*.md" -o -name "*.txt" -o -name "LICENSE" -o -name ".git*" \) -delete 2>/dev/null || true

ARG PORT=8084
ENV PORT=${PORT}

ARG WAITING_SERVER_PORT=8085
ENV WAITING_SERVER_PORT=${WAITING_SERVER_PORT}

ENV GO_SPIN_DATA_BASE_URL="https://container.mydomain.com"
ENV GO_SPIN_DATA_SPIN_UP_URL="https://up.mydomain.com/container"

# Default UID/GID for the runtime user (overridable at `docker run -e UID=... -e GID=...`)
ENV UID=1000
ENV GID=1000

# Copy entrypoint that ensures user/group exist and runs the process as that user
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]

EXPOSE ${PORT}
EXPOSE ${WAITING_SERVER_PORT}
CMD ["./main"]

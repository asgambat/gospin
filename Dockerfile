FROM golang:1.25.6-alpine AS build
#RUN apk add --no-cache curl libstdc++ libgcc alpine-sdk
RUN apk add --no-cache ca-certificates

ARG GOARCH=arm64

WORKDIR /app

#RUN curl -sL https://github.com/tailwindlabs/tailwindcss/releases/latest/download/tailwindcss-linux-x64-musl -o tailwindcss
#RUN chmod +x tailwindcss
#RUN go install github.com/a-h/templ/cmd/templ@latest

COPY go.mod go.sum ./
RUN go mod download

COPY . .

#RUN templ generate
#RUN ./tailwindcss -i cmd/web/styles/input.css -o cmd/web/assets/css/output.css
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${GOARCH} GOMAXPROCS=2 go build -trimpath -ldflags="-s -w" -gcflags="all=-N -l" -o /app/main ./cmd/server/main.go

#FROM gcr.io/distroless/static-debian11 AS prod
FROM alpine:3.20.1 AS prod
RUN apk add --no-cache su-exec

WORKDIR /app

COPY --from=build /app/main /app/main
COPY --from=build /app/ui /app/ui
COPY --from=build /app/config /app/config

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

# Build stage - only used for local builds
FROM golang:1.23-alpine AS builder

# Accept build arguments
ARG VERSION=dev
ARG COMMIT=dev
ARG DATE=unknown

WORKDIR /app
RUN apk --no-cache update && apk --no-cache upgrade && apk --no-cache add git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X github.com/koltyakov/pgcopy/cmd.version=${VERSION} -X github.com/koltyakov/pgcopy/cmd.commit=${COMMIT} -X github.com/koltyakov/pgcopy/cmd.date=${DATE}" \
    -o pgcopy .

# Runtime stage for GoReleaser
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata
RUN adduser -D -s /bin/sh pgcopy
COPY pgcopy /usr/local/bin/pgcopy
RUN chmod +x /usr/local/bin/pgcopy
USER pgcopy
WORKDIR /home/pgcopy
ENTRYPOINT ["/usr/local/bin/pgcopy"]
CMD ["--help"]

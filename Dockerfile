# Build stage
FROM golang:1.22.10-alpine3.18 AS builder

WORKDIR /app
RUN apk --no-cache update && apk --no-cache upgrade && apk --no-cache add git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o pgcopy ./main.go

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata
RUN adduser -D -s /bin/sh pgcopy
COPY --from=builder /app/pgcopy /usr/local/bin/pgcopy
RUN chmod +x /usr/local/bin/pgcopy
USER pgcopy
WORKDIR /home/pgcopy
ENTRYPOINT ["/usr/local/bin/pgcopy"]
CMD ["--help"]

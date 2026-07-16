FROM golang:1.26.0 AS builder

WORKDIR /app

ENV GOPROXY=direct
ENV GOSUMDB=off

COPY go.mod go.sum ./

RUN go mod download && go mod verify

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /app/operator ./cmd/manager

FROM alpine:latest

WORKDIR /

RUN apk add --no-cache ca-certificates

COPY --from=builder /app/operator /app/operator

ENV GIN_MODE=release \
    HOME=/tmp \
    HELM_CACHE_HOME=/tmp/helm/cache \
    HELM_CONFIG_HOME=/tmp/helm/config \
    HELM_DATA_HOME=/tmp/helm/data
RUN chmod +x /app/operator

USER 65532:65532

ENTRYPOINT ["/app/operator"]

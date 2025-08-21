FROM golang:1.24.5 as builder

WORKDIR /app

ENV GOPROXY=direct
ENV GOSUMDB=off

COPY go.mod go.sum ./

RUN go mod download && go mod verify

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /app/operator ./main.go

FROM alpine:latest

WORKDIR /

RUN apk update && apk add --no-cache ca-certificates && apk add curl openssl bash kubectl helm

COPY --from=builder /app/operator /app/operator

ENV GIN_MODE release
RUN chmod +x /app/operator

EXPOSE 3000

ENTRYPOINT ["/app/operator"]
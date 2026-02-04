FROM golang:1.25-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o headfwd-sidecar .

FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY --from=builder /build/headfwd-sidecar /usr/local/bin/
ENTRYPOINT ["headfwd-sidecar"]

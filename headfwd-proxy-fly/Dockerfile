FROM golang:1.25-alpine AS builder

WORKDIR /build
COPY go.mod ./
RUN go mod download
COPY . .
RUN go build -o headfwd-proxy-fly .

FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY --from=builder /build/headfwd-proxy-fly /usr/local/bin/
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["headfwd-proxy-fly"]

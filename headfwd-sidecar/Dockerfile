FROM node:22-alpine AS frontend
WORKDIR /app/portal/frontend
COPY portal/frontend/package*.json ./
RUN npm ci
COPY portal/frontend/ ./
RUN npx vite build

FROM golang:1.25-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/portal/frontend/dist ./portal/frontend/dist
RUN CGO_ENABLED=0 go build -o headfwd-sidecar .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /build/headfwd-sidecar /usr/local/bin/
ENTRYPOINT ["headfwd-sidecar"]

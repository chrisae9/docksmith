# Stage 1: Build React frontend
FROM node:24-alpine3.22 AS frontend-builder

WORKDIR /build/ui
COPY ui/package*.json ./
RUN npm ci
COPY ui/ ./
RUN npm run build

# Stage 2: Build Go backend
FROM golang:1.25-alpine3.22 AS backend-builder

WORKDIR /build
COPY go.mod go.sum ./
ENV GOTOOLCHAIN=auto
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o docksmith ./cmd/docksmith

# Stage 3: Runtime
FROM alpine:3.23

RUN apk add --no-cache ca-certificates bash curl jq docker-cli docker-cli-compose

# Create docker group with GID 972 (matches host) and docksmith user
RUN addgroup -g 972 docker && \
    addgroup -g 1000 docksmith && \
    adduser -D -u 1000 -G docksmith docksmith && \
    adduser docksmith docker

RUN mkdir -p /data && chown docksmith:docksmith /data

WORKDIR /app

COPY --from=backend-builder /build/docksmith /app/docksmith
COPY --from=frontend-builder /build/ui/dist /app/ui/dist

RUN chown -R docksmith:docksmith /app

USER docksmith

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/api/health || exit 1

ENTRYPOINT ["/app/docksmith"]
CMD ["api", "--port", "8080"]

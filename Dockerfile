# ===== Stage 1: build web UI =====
FROM node:22-alpine AS web
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ===== Stage 2: build Go server =====
FROM golang:1.23-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /keo-server ./cmd/server

# ===== Stage 3: runtime tối giản =====
FROM alpine:3.20
RUN adduser -D keo
USER keo
COPY --from=build /keo-server /keo-server
COPY --from=web /app/web/dist /web/dist
ENV WEB_DIST=/web/dist LISTEN_ADDR=:8080
EXPOSE 8080
ENTRYPOINT ["/keo-server"]

FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /showlib ./cmd/showlib

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=build /showlib /usr/local/bin/showlib
WORKDIR /app
ENV SHOWLIB_LISTEN_ADDR=0.0.0.0:8080 \
    SHOWLIB_DB_PATH=/app/data/showlib.sqlite
EXPOSE 8080
CMD ["showlib"]

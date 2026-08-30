FROM --platform=$BUILDPLATFORM golang:1.27.0-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS TARGETARCH VERSION=dev-build

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
      -ldflags="-s -w -X 'element-agent/cmd.AppVersion=${VERSION}'" \
      -o /app/element-agent .

FROM alpine:3.24.1

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -g 10001 -S app && \
    adduser -u 10001 -S -G app app

WORKDIR /app
COPY --from=builder --chown=10001:10001 /app/element-agent .

RUN mkdir -p /data /etc/element-agent && chown 10001:10001 /data

VOLUME ["/data"]

USER 10001:10001
EXPOSE 9000
ENTRYPOINT ["./element-agent"]
CMD ["server", "--config", "/etc/element-agent/config.yaml"]

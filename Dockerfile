# 构建阶段
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X serverpanel/internal/version.Version=$(cat VERSION)" \
      -o /out/center ./cmd/center \
 && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/vm-collect ./cmd/vm-collect

# 运行阶段（需要 ssh 客户端去连宿主机 / 虚拟机）
FROM alpine:3.20
RUN apk add --no-cache ca-certificates openssh-client
WORKDIR /opt/server-panel
COPY --from=build /out/center /out/vm-collect ./
EXPOSE 8080
VOLUME ["/opt/server-panel/data"]
ENTRYPOINT ["/opt/server-panel/center", "-config", "/opt/server-panel/center.json"]

# mplay 服务端镜像：单个静态二进制 + 内嵌前端
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=1.0.0
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/mplay .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -u 10001 -h /data mplay \
 && mkdir -p /data /media \
 && chown -R mplay:mplay /data /media
COPY --from=build /out/mplay /usr/local/bin/mplay
USER mplay
ENV HOME=/data \
    MPLAY_ADDR=:8737 \
    MPLAY_MEDIA=/media \
    TZ=Asia/Shanghai
VOLUME ["/media", "/data"]
EXPOSE 8737
ENTRYPOINT ["mplay"]

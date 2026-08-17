# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# 容器内串口不可用（CGO），构建静态二进制；mock/native 能力完全可用
RUN CGO_ENABLED=0 go build -o /out/devagent ./cmd/devagent/

FROM alpine:3.20
RUN apk add --no-cache tzdata
COPY --from=build /out/devagent /usr/local/bin/devagent
COPY configs/ /app/configs/
WORKDIR /app
EXPOSE 8080
ENTRYPOINT ["devagent"]

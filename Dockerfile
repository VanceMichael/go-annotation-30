# 微短剧内容审核与分账结算平台 —— 多阶段构建
# 同时支持 linux/amd64 与 linux/arm64。
FROM golang:1.22 AS build

ENV GOTOOLCHAIN=local \
    CGO_ENABLED=0

WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .

RUN go build -trimpath -ldflags "-s -w" -o /out/dramactl ./cmd/dramactl

FROM gcr.io/distroless/static-debian12:nonroot AS runtime

WORKDIR /app

COPY --from=build /out/dramactl /usr/local/bin/dramactl

USER nonroot:nonroot

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/dramactl"]
CMD ["selfcheck"]

FROM golang:1.26.5-alpine3.23@sha256:622e56dbc11a8cfe87cafa2331e9a201877271cbff918af53d3be315f3da88cc AS build
WORKDIR /src
COPY go.mod ./
COPY . .
ARG VERSION=0.1.0-dev
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false -ldflags="-s -w -X github.com/torgnexa/torgnexa/internal/platform/domain.version=${VERSION}" -o /out/torgnexa-api ./cmd/api \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false -ldflags="-s -w -X github.com/torgnexa/torgnexa/internal/platform/domain.version=${VERSION}" -o /out/torgnexa-worker ./cmd/worker \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false -ldflags="-s -w -X github.com/torgnexa/torgnexa/internal/platform/domain.version=${VERSION}" -o /out/torgnexa-scheduler ./cmd/scheduler \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false -ldflags="-s -w -X github.com/torgnexa/torgnexa/internal/platform/domain.version=${VERSION}" -o /out/torgnexa-mcp ./cmd/mcp \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false -ldflags="-s -w -X github.com/torgnexa/torgnexa/internal/platform/domain.version=${VERSION}" -o /out/torgnexa-runtime-qualifier ./cmd/torgnexa-runtime-qualifier

FROM scratch AS runtime
WORKDIR /app
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/ /app/
USER 10001:10001
ENTRYPOINT ["/app/torgnexa-api"]

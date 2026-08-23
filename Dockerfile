FROM golang:1.26.7-alpine3.23@sha256:b17af760035fc2f338eed92d448a6c67f2d45438844fc6c60678fa5f99e44b57 AS build
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

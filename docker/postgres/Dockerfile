# syntax=docker/dockerfile:1.7

# Keep the official PostgreSQL runtime as the compatibility baseline, but
# refresh its Alpine packages at build time and replace the bundled gosu
# binary. The public postgres image currently ships gosu built with an older
# Go toolchain, which leaves the Go standard library visible to vulnerability
# scanners.
FROM --platform=$BUILDPLATFORM golang:1.26.7-alpine3.23@sha256:b17af760035fc2f338eed92d448a6c67f2d45438844fc6c60678fa5f99e44b57 AS gosu-build

ARG BUILDARCH
ARG TARGETOS
ARG TARGETARCH

ADD --checksum=sha256:cd9719b775dbfedae53923c9b0dc792b66d42c51e0b36652ed6f747fbadc0164 \
    https://github.com/tianon/gosu/archive/refs/tags/1.19.tar.gz /tmp/gosu.tar.gz

RUN set -eux; \
    mkdir -p /src /out; \
    tar -xzf /tmp/gosu.tar.gz --strip-components=1 -C /src; \
    test "$(sed -n 's/.*Version = "\([0-9.]*\)".*/\1/p' /src/version.go)" = 1.19; \
    cd /src; \
    go mod download; \
    CGO_ENABLED=0 GOOS="${TARGETOS:-linux}" GOARCH="$TARGETARCH" \
      go build -trimpath -buildvcs=false -ldflags='-s -w' -o /out/gosu .; \
    test -x /out/gosu; \
    if [ "$BUILDARCH" = "$TARGETARCH" ]; then /out/gosu --version; fi

FROM scratch AS gosu-artifact
COPY --from=gosu-build /out/gosu /gosu

FROM postgres:18.6-alpine3.24@sha256:d3e1620b530c944afa6e887d22eb899824da68e19c52024bf98f5220c88a65b2

# Pull the current Alpine security fixes into the derived runtime image.
RUN apk upgrade --no-cache

COPY --from=gosu-build /out/gosu /usr/local/bin/gosu
ARG BUILDARCH
ARG TARGETARCH
RUN chmod 0755 /usr/local/bin/gosu; \
    if [ "$BUILDARCH" = "$TARGETARCH" ]; then /usr/local/bin/gosu --version; fi

# The upstream entrypoint supports starting as the database account. Keeping
# the final image non-root also covers the psql-only migration/init use cases.
USER postgres

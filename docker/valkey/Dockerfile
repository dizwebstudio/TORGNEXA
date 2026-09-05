# syntax=docker/dockerfile:1.7

# Refresh the Alpine security packages in the official Valkey runtime. The
# current upstream image has an outdated setpriv binary even though OpenSSL is
# already patched; apk upgrade brings all packages to the current repository
# state without changing Valkey's data layout or entrypoint.
FROM valkey/valkey:9.1.2-alpine@sha256:ccfa19b0d743e48927e1c8c14e39e0acb97b5cea347fef0bfe340247fea920cd

USER root

RUN set -eux; \
    apk upgrade --no-cache; \
    for expected in \
      'libcrypto3=3.5.8-r0' \
      'libssl3=3.5.8-r0' \
      'openssl=3.5.8-r0' \
      'setpriv=2.42.3-r1'; do \
      package=${expected%%=*}; \
      version=${expected#*=}; \
      installed=$(apk info -e -v "$package" | sed -n "s/^${package}-//p" | head -n 1); \
      test "$installed" = "$version" || { echo "unexpected $package version: $installed (want $version)" >&2; exit 1; }; \
    done

# Do not rely on Compose's numeric user override for the default runtime.
USER valkey

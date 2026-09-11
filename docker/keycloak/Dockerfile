# syntax=docker/dockerfile:1.7

# Keep the vendor-built Keycloak distribution and its pinned release, but use
# a current standalone UBI JRE for the runtime. The official Keycloak image's
# RPM-packaged JDK is the source of a libpng H finding without a Red Hat fixed
# version; the Temurin JRE is not represented as that vulnerable RPM package.
FROM quay.io/keycloak/keycloak:26.7.3@sha256:ff4257d0d64efbe99ed1ddfaf07765cc3c36dc7518bf8324d41961327f441c54 AS keycloak-dist

FROM scratch AS keycloak-dependencies

ADD --checksum=sha256:e36f5237c1267983e5b88dc2169f6b9d7e50eceec6dc1ca31018e3877e14af66 \
    https://repo1.maven.org/maven2/com/microsoft/sqlserver/mssql-jdbc/13.4.0.jre11/mssql-jdbc-13.4.0.jre11.jar \
    /deps/com.microsoft.sqlserver.mssql-jdbc-13.2.1.jre11.jar

# Keycloak 26.7.3 ships netty-handler 4.1.136.Final, which is affected by
# CVE-2026-75595. Keep the distribution's generated classpath filename while
# replacing its contents with the upstream fixed patch release.
ADD --checksum=sha256:d0e4c6ee4779f59f6ab2fb5d388e4f57147c82270164b37945764bb9bda96a44 \
    https://repo1.maven.org/maven2/io/netty/netty-handler/4.1.137.Final/netty-handler-4.1.137.Final.jar \
    /deps/io.netty.netty-handler-4.1.136.Final.jar

FROM eclipse-temurin:21-jre-ubi9-minimal@sha256:4009391968b550c1e325a7dfa9e75106cabd44814780861923e553baf56c9560

USER root

COPY --from=keycloak-dist --chown=1000:0 /opt/keycloak /opt/keycloak
COPY --from=keycloak-dependencies /deps/com.microsoft.sqlserver.mssql-jdbc-13.2.1.jre11.jar /opt/keycloak/lib/lib/main/com.microsoft.sqlserver.mssql-jdbc-13.2.1.jre11.jar
COPY --from=keycloak-dependencies /deps/io.netty.netty-handler-4.1.136.Final.jar /opt/keycloak/lib/lib/main/io.netty.netty-handler-4.1.136.Final.jar
COPY docker/keycloak/entrypoint.sh /usr/local/bin/torgnexa-keycloak-entrypoint

RUN set -eux; \
    chmod 0755 /usr/local/bin/torgnexa-keycloak-entrypoint; \
    chmod 0644 /opt/keycloak/lib/lib/main/com.microsoft.sqlserver.mssql-jdbc-13.2.1.jre11.jar; \
    chmod 0644 /opt/keycloak/lib/lib/main/io.netty.netty-handler-4.1.136.Final.jar; \
    chown 1000:0 /opt/keycloak/lib/lib/main/com.microsoft.sqlserver.mssql-jdbc-13.2.1.jre11.jar; \
    chown 1000:0 /opt/keycloak/lib/lib/main/io.netty.netty-handler-4.1.136.Final.jar; \
    microdnf --refresh --noplugins upgrade -y; \
    microdnf clean all; \
    remove_packages=; \
    for package in \
      curl-minimal libcurl-minimal libpng \
      elfutils-debuginfod-client freetype harfbuzz fontconfig binutils \
      librepo rpm rpm-libs libdnf microdnf; do \
      if rpm -q "$package" >/dev/null 2>&1; then \
        remove_packages="$remove_packages $package"; \
      fi; \
    done; \
    # These packages are not needed by Keycloak's PostgreSQL runtime. Remove
    # them together with their package metadata; some are only dependencies of
    # the package manager or the unused font/JDK desktop support. \
    rpm -e --nodeps $remove_packages; \
    rm -rf /var/cache/dnf /var/cache/yum; \
    test -f /opt/keycloak/lib/lib/main/com.microsoft.sqlserver.mssql-jdbc-13.2.1.jre11.jar; \
    test "$(sha256sum /opt/keycloak/lib/lib/main/io.netty.netty-handler-4.1.136.Final.jar | cut -d ' ' -f 1)" = d0e4c6ee4779f59f6ab2fb5d388e4f57147c82270164b37945764bb9bda96a44; \
    test -f /opt/keycloak/bin/kc.sh; \
    test -x /opt/keycloak/bin/kc.sh

# This deployment uses PostgreSQL. The optional SQL Server JDBC driver is not
# configured, but Keycloak's generated classpath metadata requires the path to
# exist. Keep that path and replace the bundled driver with a patched Maven
# artifact instead of shipping the vulnerable version.
ENV KC_RUN_IN_CONTAINER=true
WORKDIR /opt/keycloak
USER 1000:0
ENTRYPOINT ["/usr/local/bin/torgnexa-keycloak-entrypoint"]
CMD ["start-dev"]

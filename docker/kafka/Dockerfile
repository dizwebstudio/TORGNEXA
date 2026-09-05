# syntax=docker/dockerfile:1.7

# The official Kafka image currently contains vulnerable Alpine packages and
# bundled Java libraries. Keep the Kafka runtime and entrypoint from Apache,
# but refresh both dependency layers in a reproducible derived image.
FROM scratch AS kafka-dependencies

ADD --checksum=sha256:4b40a06396f239f8de2da57419adde6e94e5edc18a2171d471ea05eeed4e5c2d \
    https://repo1.maven.org/maven2/com/fasterxml/jackson/core/jackson-core/2.21.4/jackson-core-2.21.4.jar \
    /deps/jackson-core-2.21.4.jar
ADD --checksum=sha256:3888e9e69ab66fbacaacc9aea0e9ffbf15368288e4aca468b024dba11c09fbf9 \
    https://repo1.maven.org/maven2/com/fasterxml/jackson/core/jackson-databind/2.21.4/jackson-databind-2.21.4.jar \
    /deps/jackson-databind-2.21.4.jar
ADD --checksum=sha256:40f3fc22e9b1575838cc256013bc17db5b824c79fe0b5a32ae1662201e99dcca \
    https://repo1.maven.org/maven2/com/fasterxml/jackson/dataformat/jackson-dataformat-csv/2.21.4/jackson-dataformat-csv-2.21.4.jar \
    /deps/jackson-dataformat-csv-2.21.4.jar
ADD --checksum=sha256:055eb4c008ab12f0cb63e93a6454463d032fde0fca85943d69f8dc7469489e4b \
    https://repo1.maven.org/maven2/com/fasterxml/jackson/dataformat/jackson-dataformat-yaml/2.21.4/jackson-dataformat-yaml-2.21.4.jar \
    /deps/jackson-dataformat-yaml-2.21.4.jar
ADD --checksum=sha256:2dde4a8f1aad3306c040f56ff40da81c6d58dcef3434c00849c9bce09f56a074 \
    https://repo1.maven.org/maven2/com/fasterxml/jackson/datatype/jackson-datatype-jdk8/2.21.4/jackson-datatype-jdk8-2.21.4.jar \
    /deps/jackson-datatype-jdk8-2.21.4.jar
ADD --checksum=sha256:ce62827a3381ef2db712503e242ab5a51a881613a9446948a3c25b2d7df326fe \
    https://repo1.maven.org/maven2/com/fasterxml/jackson/jakarta/rs/jackson-jakarta-rs-base/2.21.4/jackson-jakarta-rs-base-2.21.4.jar \
    /deps/jackson-jakarta-rs-base-2.21.4.jar
ADD --checksum=sha256:a8cd69b1dac55bdd0bad9d9803157b296243987b08d5ed7ccde3c81c07ce05cc \
    https://repo1.maven.org/maven2/com/fasterxml/jackson/jakarta/rs/jackson-jakarta-rs-json-provider/2.21.4/jackson-jakarta-rs-json-provider-2.21.4.jar \
    /deps/jackson-jakarta-rs-json-provider-2.21.4.jar
ADD --checksum=sha256:1d8f67a6e610c349c84cb9b468334164e2b23c86e4be26f903ab8bb1d2067794 \
    https://repo1.maven.org/maven2/com/fasterxml/jackson/module/jackson-module-blackbird/2.21.4/jackson-module-blackbird-2.21.4.jar \
    /deps/jackson-module-blackbird-2.21.4.jar
ADD --checksum=sha256:d5c5798fffa760ba6a84a7c9b65caf577f92b8d1c6bccb71d75e93e2ce59bf1d \
    https://repo1.maven.org/maven2/com/fasterxml/jackson/module/jackson-module-jakarta-xmlbind-annotations/2.21.4/jackson-module-jakarta-xmlbind-annotations-2.21.4.jar \
    /deps/jackson-module-jakarta-xmlbind-annotations-2.21.4.jar

ADD --checksum=sha256:6b0e03b545fa49fc82cf3149dafb3849e6e0827b3756c31b0e1f5324d86816d2 \
    https://repo1.maven.org/maven2/org/eclipse/jetty/jetty-alpn-client/12.0.36/jetty-alpn-client-12.0.36.jar \
    /deps/jetty-alpn-client-12.0.36.jar
ADD --checksum=sha256:1e188b18e8ff6b60ca743e9c9cc39c35f8b6b1b6a7dc07cbdd06800672a1ee02 \
    https://repo1.maven.org/maven2/org/eclipse/jetty/jetty-client/12.0.36/jetty-client-12.0.36.jar \
    /deps/jetty-client-12.0.36.jar
ADD --checksum=sha256:64d42fc2e1c3fd6b7e7a77c1165626bce3a52ef3ed89a7cf00163d11b42201c8 \
    https://repo1.maven.org/maven2/org/eclipse/jetty/ee10/jetty-ee10-servlet/12.0.36/jetty-ee10-servlet-12.0.36.jar \
    /deps/jetty-ee10-servlet-12.0.36.jar
ADD --checksum=sha256:e4bc43fbe7ddb1f7658a935b9ad0329d9cf4eae446533bfed7822e6183fe933e \
    https://repo1.maven.org/maven2/org/eclipse/jetty/ee10/jetty-ee10-servlets/12.0.36/jetty-ee10-servlets-12.0.36.jar \
    /deps/jetty-ee10-servlets-12.0.36.jar
ADD --checksum=sha256:2b5c6ee1303aa979b99afb27e3c602096c344e06da9bdc0057938d1c47f8f1f2 \
    https://repo1.maven.org/maven2/org/eclipse/jetty/jetty-http/12.0.36/jetty-http-12.0.36.jar \
    /deps/jetty-http-12.0.36.jar
ADD --checksum=sha256:b191d45e3c48711cc784d509a8dec50112816af392b2b7c76d9e42324b5064da \
    https://repo1.maven.org/maven2/org/eclipse/jetty/jetty-io/12.0.36/jetty-io-12.0.36.jar \
    /deps/jetty-io-12.0.36.jar
ADD --checksum=sha256:e6fb70974291312c58ac84d8bf28c909d1800d1b3104020e7cd4db77391f9fb2 \
    https://repo1.maven.org/maven2/org/eclipse/jetty/jetty-security/12.0.36/jetty-security-12.0.36.jar \
    /deps/jetty-security-12.0.36.jar
ADD --checksum=sha256:f0eb8d1e71d67d0f937c45752546cf479ba55ad3135cf600d3bf723f52cd53fe \
    https://repo1.maven.org/maven2/org/eclipse/jetty/jetty-server/12.0.36/jetty-server-12.0.36.jar \
    /deps/jetty-server-12.0.36.jar
ADD --checksum=sha256:5b6dc693b9e3d87d309027a5f506b57225b6450444a3f276d9089f2c663cbcc4 \
    https://repo1.maven.org/maven2/org/eclipse/jetty/jetty-session/12.0.36/jetty-session-12.0.36.jar \
    /deps/jetty-session-12.0.36.jar
ADD --checksum=sha256:e16c145dd761168133d1505b6f7cf05343f5e3e3ff03333757c8a6312896ab48 \
    https://repo1.maven.org/maven2/org/eclipse/jetty/jetty-util/12.0.36/jetty-util-12.0.36.jar \
    /deps/jetty-util-12.0.36.jar
ADD --checksum=sha256:15731604f27ce9045eeb977f9760d766ffa1a53734b9adb31a6e493ec76bc3a2 \
    https://repo1.maven.org/maven2/org/jline/jline/3.30.14/jline-3.30.14.jar \
    /deps/jline-3.30.14.jar

FROM apache/kafka:4.3.1@sha256:77e3df9054047a88b520d0cc46e16696d3b22022e1d580aeccd2632df6532837

USER root

COPY --from=kafka-dependencies /deps /tmp/kafka-dependencies

RUN set -eux; \
    apk upgrade --no-cache; \
    for expected in \
      'libcrypto3=3.5.8-r0' \
      'libssl3=3.5.8-r0' \
      'openssl=3.5.8-r0' \
      'libexpat=2.8.4-r0' \
      'p11-kit=0.26.2-r0' \
      'p11-kit-trust=0.26.2-r0' \
      'sqlite-libs=3.53.4-r0'; do \
      package=${expected%%=*}; \
      version=${expected#*=}; \
      installed=$(apk info -e -v "$package" | sed -n "s/^${package}-//p" | head -n 1); \
      test "$installed" = "$version" || { echo "unexpected $package version: $installed (want $version)" >&2; exit 1; }; \
    done; \
    for jar in \
      jackson-core-2.21.2.jar \
      jackson-databind-2.21.2.jar \
      jackson-dataformat-csv-2.21.2.jar \
      jackson-dataformat-yaml-2.21.2.jar \
      jackson-datatype-jdk8-2.21.2.jar \
      jackson-jakarta-rs-base-2.21.2.jar \
      jackson-jakarta-rs-json-provider-2.21.2.jar \
      jackson-module-blackbird-2.21.2.jar \
      jackson-module-jakarta-xmlbind-annotations-2.21.2.jar \
      jetty-alpn-client-12.0.34.jar \
      jetty-client-12.0.34.jar \
      jetty-ee10-servlet-12.0.34.jar \
      jetty-ee10-servlets-12.0.34.jar \
      jetty-http-12.0.34.jar \
      jetty-io-12.0.34.jar \
      jetty-security-12.0.34.jar \
      jetty-server-12.0.34.jar \
      jetty-session-12.0.34.jar \
      jetty-util-12.0.34.jar \
      jline-3.30.4.jar; do \
      rm -f "/opt/kafka/libs/$jar"; \
    done; \
    cp /tmp/kafka-dependencies/*.jar /opt/kafka/libs/; \
    chown appuser:appuser /opt/kafka/libs/*.jar; \
    rm -rf /tmp/kafka-dependencies; \
    test -f /opt/kafka/libs/jackson-core-2.21.4.jar; \
    test -f /opt/kafka/libs/jackson-databind-2.21.4.jar; \
    test -f /opt/kafka/libs/jetty-security-12.0.36.jar; \
    test -f /opt/kafka/libs/jline-3.30.14.jar

USER appuser

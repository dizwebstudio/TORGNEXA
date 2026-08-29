ARG OPENCART_VERSION=4.1.0.4
ARG OPENCART_SHA256=cdef537f5a0a7e1b4ddbe16f589dd2e80b10150a09bfbe93ce76ef2d5c3218c0

FROM php:8.3-apache@sha256:060ed9c0f6e4bbe4f8b25a34ca1ec596b96d8f4011cf7ee7eb6b7eecf01cb74f

ARG OPENCART_VERSION
ARG OPENCART_SHA256

RUN apt-get update \
    && apt-get install -y --no-install-recommends curl unzip libfreetype6-dev libjpeg62-turbo-dev libpng-dev libicu-dev libzip-dev \
    && docker-php-ext-configure gd --with-freetype --with-jpeg \
    && docker-php-ext-install -j"$(nproc)" gd intl mysqli opcache pdo_mysql zip \
    && a2enmod rewrite headers \
    && printf '%s\n' 'SetEnvIf Authorization "^(.*)$" HTTP_AUTHORIZATION=$1' > /etc/apache2/conf-available/torgnexa.conf \
    && a2enconf torgnexa \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /var/www/html

RUN set -eux; \
    curl -fsSL "https://github.com/opencart/opencart/releases/download/${OPENCART_VERSION}/opencart-${OPENCART_VERSION}.zip" -o /tmp/opencart.zip; \
    echo "${OPENCART_SHA256}  /tmp/opencart.zip" | sha256sum -c -; \
    unzip -q /tmp/opencart.zip -d /tmp/opencart; \
    cp -a /tmp/opencart/upload/. /var/www/html/; \
    rm -rf /tmp/opencart /tmp/opencart.zip; \
    cp config-dist.php config.php; \
    cp admin/config-dist.php admin/config.php; \
    mkdir -p system/storage/cache system/storage/download system/storage/logs system/storage/session system/storage/upload; \
    chown -R www-data:www-data /var/www/html

# The extension is copied into the exact path OpenCart derives from
# torgnexa.ocmod.zip. This makes the disposable stack test the same route
# that a real Extensions -> Installer upload enables.
COPY connectors/storefronts/opencart/extension/torgnexa /var/www/html/extension/torgnexa
COPY docker/opencart-test/entrypoint.sh /usr/local/bin/torgnexa-opencart-entrypoint
COPY docker/opencart-test/configure.php /usr/local/bin/torgnexa-opencart-configure.php
COPY docker/opencart-test/seed-demo.php /usr/local/bin/torgnexa-opencart-seed.php
COPY docker/opencart-test/seed.sql /usr/local/share/torgnexa-opencart-seed.sql

RUN chmod +x /usr/local/bin/torgnexa-opencart-entrypoint \
    && chown -R www-data:www-data /var/www/html/extension/torgnexa

ENTRYPOINT ["/usr/local/bin/torgnexa-opencart-entrypoint"]
CMD ["apache2-foreground"]

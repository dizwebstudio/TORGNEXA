FROM wordpress:6.8.2-php8.3-apache@sha256:09ac1315368f234db7559e4f9dcca3178a5efc6f2193b88289252abe18551522

ARG WOOCOMMERCE_VERSION=9.8.5

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl openssl unzip \
    && curl -fsSL https://github.com/wp-cli/wp-cli/releases/download/v2.11.0/wp-cli-2.11.0.phar -o /usr/local/bin/wp \
    && chmod +x /usr/local/bin/wp \
    && curl -fsSL "https://downloads.wordpress.org/plugin/woocommerce.${WOOCOMMERCE_VERSION}.zip" -o /tmp/woocommerce.zip \
    && unzip -q /tmp/woocommerce.zip -d /usr/src/wordpress/wp-content/plugins \
    && rm -f /tmp/woocommerce.zip \
    && a2enmod rewrite headers ssl \
    && a2ensite default-ssl \
    && openssl req -x509 -nodes -newkey rsa:2048 -days 3650 \
      -keyout /etc/ssl/private/ssl-cert-snakeoil.key \
      -out /etc/ssl/certs/ssl-cert-snakeoil.pem \
      -subj "/CN=localhost" \
      -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" \
    && printf '%s\n' 'SetEnvIf Authorization "^(.*)$" HTTP_AUTHORIZATION=$1' > /etc/apache2/conf-available/torgnexa-auth.conf \
    && a2enconf torgnexa-auth \
    && rm -rf /var/lib/apt/lists/*

COPY docker/woocommerce-test/entrypoint.sh /usr/local/bin/torgnexa-woocommerce-entrypoint
COPY docker/woocommerce-test/seed-demo.php /usr/local/bin/torgnexa-woocommerce-seed.php
COPY docker/woocommerce-test/local-rest-canonical.php /usr/src/wordpress/wp-content/mu-plugins/torgnexa-local-rest-canonical.php

RUN chmod +x /usr/local/bin/torgnexa-woocommerce-entrypoint

ENTRYPOINT ["/usr/local/bin/torgnexa-woocommerce-entrypoint"]
CMD ["apache2-foreground"]

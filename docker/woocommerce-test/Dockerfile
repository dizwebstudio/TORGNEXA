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
    && sed -ri 's!^[[:space:]]*Listen 80$!Listen 8080!; s!^[[:space:]]*Listen 443$!Listen 8443!' /etc/apache2/ports.conf \
    && sed -ri 's!<VirtualHost \*:80>!<VirtualHost *:8080>!; s!<VirtualHost \*:443>!<VirtualHost *:8443>!' /etc/apache2/sites-enabled/000-default.conf /etc/apache2/sites-enabled/default-ssl.conf \
    && openssl req -x509 -nodes -newkey rsa:2048 -days 3650 \
      -keyout /etc/ssl/private/ssl-cert-snakeoil.key \
      -out /etc/ssl/certs/ssl-cert-snakeoil.pem \
      -subj "/CN=localhost" \
      -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" \
    && printf '%s\n' 'SetEnvIf Authorization "^(.*)$" HTTP_AUTHORIZATION=$1' > /etc/apache2/conf-available/torgnexa-auth.conf \
    && a2enconf torgnexa-auth \
    && chmod 0640 /etc/ssl/private/ssl-cert-snakeoil.key \
    && chown root:www-data /etc/ssl/private/ssl-cert-snakeoil.key \
    && chmod 0750 /etc/ssl/private \
    && chown root:www-data /etc/ssl/private \
    && mkdir -p /var/run/apache2 /var/lock/apache2 /var/log/apache2 \
    && chown -R www-data:www-data /var/run/apache2 /var/lock/apache2 /var/log/apache2 \
    && rm -rf /var/lib/apt/lists/*

COPY docker/woocommerce-test/entrypoint.sh /usr/local/bin/torgnexa-woocommerce-entrypoint
COPY docker/woocommerce-test/seed-demo.php /usr/local/bin/torgnexa-woocommerce-seed.php
COPY docker/woocommerce-test/local-rest-canonical.php /usr/src/wordpress/wp-content/mu-plugins/torgnexa-local-rest-canonical.php

RUN chmod +x /usr/local/bin/torgnexa-woocommerce-entrypoint \
    && chown -R www-data:www-data /usr/src/wordpress/wp-content

ENTRYPOINT ["/usr/local/bin/torgnexa-woocommerce-entrypoint"]
CMD ["apache2-foreground"]
USER www-data

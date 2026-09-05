FROM prestashop/prestashop:8.1-apache@sha256:10a0fe50c46315b32cc4be0a66c06f0fbe85bbfeb048fa71d91b89295d536220

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && sed -ri 's!^Listen 80$!Listen 8080!' /etc/apache2/ports.conf \
    && sed -ri 's!<VirtualHost \*:80>!<VirtualHost *:8080>!' /etc/apache2/sites-enabled/000-default.conf \
    && sed 's/runuser -g www-data -u www-data -- php/php/' /tmp/docker_run.sh > /usr/local/bin/torgnexa-prestashop-entrypoint \
    && chmod +x /usr/local/bin/torgnexa-prestashop-entrypoint \
    && mkdir -p /var/run/apache2 /var/lock/apache2 /var/log/apache2 \
    && chown -R www-data:www-data /var/www/html /var/run/apache2 /var/lock/apache2 /var/log/apache2 \
    && rm -rf /var/lib/apt/lists/*

COPY docker/prestashop-test/seed-demo.php /usr/local/bin/torgnexa-prestashop-seed.php
COPY docker/prestashop-test/seed-demo.sh /tmp/init-scripts/20-torgnexa-prestashop-seed.sh

RUN chmod +x /tmp/init-scripts/20-torgnexa-prestashop-seed.sh

USER www-data
CMD ["/usr/local/bin/torgnexa-prestashop-entrypoint"]

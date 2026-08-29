FROM prestashop/prestashop:8.1-apache@sha256:10a0fe50c46315b32cc4be0a66c06f0fbe85bbfeb048fa71d91b89295d536220

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*

COPY docker/prestashop-test/seed-demo.php /usr/local/bin/torgnexa-prestashop-seed.php
COPY docker/prestashop-test/seed-demo.sh /tmp/init-scripts/20-torgnexa-prestashop-seed.sh

RUN chmod +x /tmp/init-scripts/20-torgnexa-prestashop-seed.sh

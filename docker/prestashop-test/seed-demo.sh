#!/bin/sh
set -eu

if [ "${TORGNEXA_PRESTASHOP_SEEDED:-0}" = "1" ]; then
  exit 0
fi

php /usr/local/bin/torgnexa-prestashop-seed.php

# Compile the dedicated Webservice Symfony container before Apache accepts
# concurrent requests; otherwise the first health-check and smoke request can
# race while both try to create the same cache file.
php -r 'require "/var/www/html/config/config.inc.php"; require "/var/www/html/init.php"; $class = "PrestaShop" . chr(92) . "PrestaShop" . chr(92) . "Adapter" . chr(92) . "ContainerBuilder"; $class::getContainer("webservice", false);'

# Init scripts run as root, while Apache serves requests as www-data. The
# warm-up creates cache files, so hand ownership back before the storefront is
# reachable.
chown -R www-data:www-data /var/www/html/var/cache

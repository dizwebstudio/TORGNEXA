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

# The image runs both initialization and Apache as www-data. Keep this guard so
# the script remains safe if an operator deliberately runs it as root.
if [ "$(id -u)" -eq 0 ]; then
  chown -R www-data:www-data /var/www/html/var/cache
fi

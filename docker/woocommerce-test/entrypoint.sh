#!/bin/sh
set -eu

: "${WORDPRESS_DB_HOST:=db:3306}"
: "${WORDPRESS_DB_USER:=wordpress}"
: "${WORDPRESS_DB_PASSWORD:=wordpress-demo}"
: "${WORDPRESS_DB_NAME:=wordpress}"
: "${WORDPRESS_SITEURL:=http://127.0.0.1:8096}"
: "${WORDPRESS_TITLE:=TORGNEXA Demo Store}"
: "${WORDPRESS_ADMIN_USER:=admin}"
: "${WORDPRESS_ADMIN_PASSWORD:=admin-demo-123}"
: "${WORDPRESS_ADMIN_EMAIL:=demo@torgnexa.local}"

export WORDPRESS_DB_HOST WORDPRESS_DB_USER WORDPRESS_DB_PASSWORD WORDPRESS_DB_NAME

# The upstream image entrypoint materializes wp-config.php and the WordPress
# files before it starts Apache. Keep it as the process we supervise so the
# test image follows the same initialization path as the official image.
/usr/local/bin/docker-entrypoint.sh apache2-foreground &
server_pid=$!
cleanup() {
  kill "$server_pid" 2>/dev/null || true
}
trap cleanup INT TERM EXIT

echo "[torgnexa] waiting for MariaDB at ${WORDPRESS_DB_HOST}"
i=0
until php -r '$parts=explode(":", getenv("WORDPRESS_DB_HOST"), 2); $host=$parts[0]; $port=(int)($parts[1] ?? 3306); $m=@new mysqli($host, getenv("WORDPRESS_DB_USER"), getenv("WORDPRESS_DB_PASSWORD"), getenv("WORDPRESS_DB_NAME"), $port); exit($m->connect_errno ? 1 : 0);'
do
  i=$((i + 1))
  if [ "$i" -gt 60 ]; then
    echo "[torgnexa] MariaDB did not become ready" >&2
    exit 1
  fi
  sleep 2
done

echo "[torgnexa] waiting for WordPress files"
i=0
until [ -f /var/www/html/wp-config.php ]
do
  i=$((i + 1))
  if [ "$i" -gt 60 ]; then
    echo "[torgnexa] wp-config.php was not generated" >&2
    exit 1
  fi
  sleep 1
done

if ! wp core is-installed --allow-root >/dev/null 2>&1; then
  echo "[torgnexa] installing WordPress"
  wp core install \
    --url="$WORDPRESS_SITEURL" \
    --title="$WORDPRESS_TITLE" \
    --admin_user="$WORDPRESS_ADMIN_USER" \
    --admin_password="$WORDPRESS_ADMIN_PASSWORD" \
    --admin_email="$WORDPRESS_ADMIN_EMAIL" \
    --skip-email \
    --allow-root
fi

wp option update blogdescription "Synthetic WooCommerce connector test store" --allow-root >/dev/null
wp option update permalink_structure '/%postname%/' --allow-root >/dev/null
wp rewrite flush --hard --allow-root >/dev/null 2>&1 || wp rewrite flush --allow-root >/dev/null
wp option update woocommerce_currency "${WOO_STORE_CURRENCY:-USD}" --allow-root >/dev/null
wp plugin activate woocommerce --allow-root >/dev/null

i=0
until wp eval 'exit(function_exists("wc_get_product") ? 0 : 1);' --allow-root >/dev/null 2>&1
do
  i=$((i + 1))
  if [ "$i" -gt 30 ]; then
    echo "[torgnexa] WooCommerce did not load" >&2
    exit 1
  fi
  sleep 1
done

if [ ! -f /var/www/html/.torgnexa-woocommerce-demo-installed ]; then
  echo "[torgnexa] loading synthetic WooCommerce catalog and order"
  wp eval-file /usr/local/bin/torgnexa-woocommerce-seed.php --allow-root
  touch /var/www/html/.torgnexa-woocommerce-demo-installed
  chown www-data:www-data /var/www/html/.torgnexa-woocommerce-demo-installed
fi

echo "[torgnexa] WooCommerce demo is available at ${WORDPRESS_SITEURL}"
wait "$server_pid"

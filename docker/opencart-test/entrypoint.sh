#!/bin/sh
set -eu

: "${DB_HOSTNAME:=db}"
: "${DB_USERNAME:=opencart}"
: "${DB_PASSWORD:=opencart-demo}"
: "${DB_DATABASE:=opencart}"
: "${DB_PORT:=3306}"
: "${DB_PREFIX:=oc_}"
: "${OPENCART_USERNAME:=admin}"
: "${OPENCART_PASSWORD:=admin-demo-123}"
: "${OPENCART_ADMIN_EMAIL:=demo@torgnexa.local}"
: "${OPENCART_HTTP_SERVER:=http://127.0.0.1:8095/}"

echo "[torgnexa] waiting for MariaDB at ${DB_HOSTNAME}:${DB_PORT}"
i=0
until php -r '$m=@new mysqli(getenv("DB_HOSTNAME"), getenv("DB_USERNAME"), getenv("DB_PASSWORD"), getenv("DB_DATABASE"), (int)getenv("DB_PORT")); exit($m->connect_errno ? 1 : 0);'
do
  i=$((i + 1))
  if [ "$i" -gt 60 ]; then
    echo "[torgnexa] MariaDB did not become ready" >&2
    exit 1
  fi
  sleep 2
done

if [ ! -f /var/www/html/.torgnexa-demo-installed ]; then
  if php -r 'mysqli_report(MYSQLI_REPORT_OFF); $m=@new mysqli(getenv("DB_HOSTNAME"), getenv("DB_USERNAME"), getenv("DB_PASSWORD"), getenv("DB_DATABASE"), (int)getenv("DB_PORT")); if ($m->connect_errno) { exit(1); } $prefix=getenv("DB_PREFIX"); $q=$m->query("SELECT 1 FROM `" . $prefix . "setting` LIMIT 1"); exit($q && $q->num_rows ? 0 : 1);'
  then
    echo "[torgnexa] existing OpenCart schema detected; skipping CLI install"
    php /usr/local/bin/torgnexa-opencart-configure.php
  else
    echo "[torgnexa] installing OpenCart ${OPENCART_VERSION:-4.x}"
    php /var/www/html/install/cli_install.php install \
      --username "$OPENCART_USERNAME" \
      --email "$OPENCART_ADMIN_EMAIL" \
      --password "$OPENCART_PASSWORD" \
      --http_server "$OPENCART_HTTP_SERVER" \
      --language en-gb \
      --db_driver mysqli \
      --db_hostname "$DB_HOSTNAME" \
      --db_username "$DB_USERNAME" \
      --db_password "$DB_PASSWORD" \
      --db_database "$DB_DATABASE" \
      --db_port "$DB_PORT" \
      --db_prefix "$DB_PREFIX"
  fi
  rm -rf /var/www/html/install
  php /usr/local/bin/torgnexa-opencart-seed.php
  touch /var/www/html/.torgnexa-demo-installed
  chown www-data:www-data /var/www/html/.torgnexa-demo-installed
  echo "[torgnexa] synthetic demo data loaded"
fi

exec "$@"

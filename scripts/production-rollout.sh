#!/bin/sh
set -eu

umask 077
export LC_ALL=C
export TZ=UTC

usage() {
  cat >&2 <<'USAGE'
usage: production-rollout.sh \
  --target ABSOLUTE_PATH \
  --release ABSOLUTE_RELEASE_PATH \
  --compose-file RELATIVE_COMPOSE_OVERLAY \
  --health-url URL \
  --version VERSION \
  --revision SHA1 \
  --archive ABSOLUTE_ARCHIVE_PATH \
  --backup-retention COUNT
USAGE
}

die() {
  echo "production-rollout: $*" >&2
  exit 1
}

target=
release=
compose_file=
health_url=
version=
revision=
archive=
backup_retention=

while [ "$#" -gt 0 ]; do
  case "$1" in
    --target)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      target=$2
      shift 2
      ;;
    --release)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      release=$2
      shift 2
      ;;
    --compose-file)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      compose_file=$2
      shift 2
      ;;
    --health-url)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      health_url=$2
      shift 2
      ;;
    --version)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      version=$2
      shift 2
      ;;
    --revision)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      revision=$2
      shift 2
      ;;
    --archive)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      archive=$2
      shift 2
      ;;
    --backup-retention)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      backup_retention=$2
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage
      die "unsupported argument: $1"
      ;;
  esac
done

[ -n "$target" ] || die "--target is required"
[ -n "$release" ] || die "--release is required"
[ -n "$compose_file" ] || die "--compose-file is required"
[ -n "$health_url" ] || die "--health-url is required"
[ -n "$version" ] || die "--version is required"
[ -n "$revision" ] || die "--revision is required"
[ -n "$archive" ] || die "--archive is required"
[ -n "$backup_retention" ] || die "--backup-retention is required"

case "$target" in
  /*) ;;
  *) die "target must be an absolute path" ;;
esac
[ "$target" != / ] || die "refusing to use / as deployment path"
case "$target" in
  */../*|*/..) die "target must not contain parent traversal" ;;
esac

case "$release" in
  "$target"/releases/*) ;;
  *) die "release must be below target/releases" ;;
esac
[ -d "$release" ] && [ ! -L "$release" ] || die "release directory is missing or unsafe"

case "$archive" in
  "$target"/incoming/*) ;;
  *) die "archive must be below target/incoming" ;;
esac
[ -f "$archive" ] && [ ! -L "$archive" ] || die "release archive is missing or unsafe"

case "$compose_file" in
  /*|*..*|*' '*|*'	'*) die "compose file must be a safe relative path" ;;
esac
case "$version" in
  ''|*[!A-Za-z0-9._-]*) die "version contains unsafe characters" ;;
esac
case "$backup_retention" in
  ''|*[!0-9]*) die "backup retention must be a positive integer" ;;
esac
[ "$backup_retention" -gt 0 ] || die "backup retention must be positive"
[ "$backup_retention" -le 99 ] || die "backup retention must not exceed 99"
printf '%s\n' "$revision" | grep -Eq '^[0-9a-f]{40}$' || die "revision is not a SHA-1"
case "$health_url" in
  http://*|https://*) ;;
  *) die "health URL must use HTTP or HTTPS" ;;
esac

for command_name in cat curl date docker find grep ln mktemp mv readlink rm sha256sum sleep sort stat tar; do
  command -v "$command_name" >/dev/null 2>&1 || die "required command not found: $command_name"
done
docker compose version >/dev/null 2>&1 || die "Docker Compose v2 is required"

[ -f "$target/.env" ] && [ ! -L "$target/.env" ] || die "production .env is missing or unsafe"
[ "$(stat -c '%a' "$target/.env")" = 600 ] || die "production .env must have mode 0600"
[ -f "$release/docker-compose.yml" ] || die "base Compose file is missing"
[ -f "$release/$compose_file" ] || die "production Compose overlay is missing"
[ -f "$release/scripts/check-production-health.sh" ] || die "health check script is missing"

rendered=$(mktemp)
backup_tmp=
metadata_tmp=

cleanup() {
  [ -z "${rendered:-}" ] || [ ! -e "$rendered" ] || rm -f -- "$rendered"
  [ -z "${backup_tmp:-}" ] || [ ! -e "$backup_tmp" ] || rm -f -- "$backup_tmp"
  [ -z "${metadata_tmp:-}" ] || [ ! -e "$metadata_tmp" ] || rm -f -- "$metadata_tmp"
}
trap cleanup EXIT HUP INT TERM

export TORGNEXA_VERSION="$version"
export TORGNEXA_RELEASE_REVISION="$revision"
docker compose --project-directory "$release" --env-file "$target/.env" \
  -f "$release/docker-compose.yml" -f "$release/$compose_file" \
  -p torgnexa-production config >"$rendered"
grep -Eq 'TORGNEXA_ENV:[[:space:]]*production([[:space:]]|$)' "$rendered" ||
  die "effective Compose configuration is not production"
grep -Eq '^  (api|torgnexa-api):$' "$rendered" ||
  die "production Compose configuration has no API service"
grep -Eq '^  (worker|torgnexa-worker):$' "$rendered" ||
  die "production Compose configuration has no worker service"
rm -f -- "$rendered"
rendered=

compose_for() {
  project=$1
  shift
  docker compose --project-directory "$project" --env-file "$target/.env" \
    -f "$project/docker-compose.yml" -f "$project/$compose_file" \
    -p torgnexa-production "$@"
}

project_image_ids() {
  containers=$(docker ps -aq --filter label=com.docker.compose.project=torgnexa-production)
  for container in $containers; do
    docker inspect --format '{{.Image}}' "$container" 2>/dev/null || true
  done | sort -u
}

previous=
if [ -L "$target/current" ]; then
  previous=$(readlink -f "$target/current" || true)
elif [ -e "$target/current" ]; then
  die "current exists but is not a symlink"
fi

if [ -n "$previous" ]; then
  case "$previous" in
    "$target"/releases/*) ;;
    *) die "current symlink points outside target/releases" ;;
  esac
  [ -d "$previous" ] && [ ! -L "$previous" ] || die "current release is missing or unsafe"
fi

old_project_images=$(project_image_ids)

backup_postgres() {
  [ -n "$previous" ] || {
    [ -z "$old_project_images" ] || die "existing project containers have no current release pointer"
    echo "no previous release; PostgreSQL backup is not needed for the initial deployment"
    return 0
  }

  backup_dir="$target/backups"
  mkdir -m 0750 -p -- "$backup_dir"
  db_containers=$(compose_for "$previous" ps -aq postgres)
  if [ -z "$db_containers" ]; then
    echo "previous release has no local postgres service; external database backup remains operator-managed"
    return 0
  fi
  running_db=$(compose_for "$previous" ps -q postgres)
  [ -n "$running_db" ] || die "previous PostgreSQL container exists but is not running; refusing unbacked rollout"

  timestamp=$(date -u '+%Y%m%dT%H%M%SZ')
  backup="$backup_dir/pre-deploy-$timestamp-$version.dump"
  [ ! -e "$backup" ] && [ ! -L "$backup" ] || die "backup path already exists: $backup"
  backup_tmp=$(mktemp "$backup_dir/.pre-deploy-$timestamp-$version.XXXXXX")
  if ! compose_for "$previous" exec -T postgres sh -eu -c \
    'PGPASSWORD="$POSTGRES_PASSWORD" pg_dump --format=custom --no-owner --no-privileges --username=torgnexa --dbname=torgnexa' >"$backup_tmp"; then
    die "PostgreSQL backup failed; refusing rollout"
  fi
  [ -s "$backup_tmp" ] || die "PostgreSQL backup is empty"
  mv -- "$backup_tmp" "$backup"
  backup_tmp=
  sha256sum -- "$backup" >"$backup.sha256"

  metadata_tmp=$(mktemp "$backup_dir/.pre-deploy-$timestamp-$version.metadata.XXXXXX")
  printf '{\n  "kind": "postgres-logical-backup",\n  "created_at": "%s",\n  "release": "%s",\n  "source_release": "%s",\n  "format": "custom"\n}\n' \
    "$timestamp" "$version" "${previous##*/}" >"$metadata_tmp"
  mv -- "$metadata_tmp" "$backup.json"
  metadata_tmp=

  count=0
  for candidate in $(find "$backup_dir" -maxdepth 1 -type f -name 'pre-deploy-*.dump' -print | sort -r); do
    count=$((count + 1))
    if [ "$count" -gt "$backup_retention" ]; then
      rm -f -- "$candidate" "$candidate.sha256" "$candidate.json"
    fi
  done
  echo "PostgreSQL backup created: $backup"
}

deploy_compose() {
  project=$1
  release_version=$2
  release_revision=$3
  export TORGNEXA_VERSION="$release_version"
  export TORGNEXA_RELEASE_REVISION="$release_revision"
  compose_for "$project" up -d --build
}

backup_postgres

ln -sfn -- "$release" "$target/current"

failed=0
if ! deploy_compose "$release" "$version" "$revision"; then
  failed=1
fi

if [ "$failed" -eq 0 ]; then
  healthy=0
  attempt=1
  while [ "$attempt" -le 30 ]; do
    if sh "$release/scripts/check-production-health.sh" "$health_url"; then
      healthy=1
      break
    fi
    sleep 5
    attempt=$((attempt + 1))
  done
  [ "$healthy" -eq 1 ] || failed=1
fi

if [ "$failed" -ne 0 ]; then
  echo "release $version failed health check; attempting rollback" >&2
  if [ -n "$previous" ] && [ -d "$previous" ]; then
    ln -sfn -- "$previous" "$target/current"
    previous_version=${previous##*/}
    previous_revision=$(cat "$previous/.release-revision" 2>/dev/null || true)
    deploy_compose "$previous" "$previous_version" "$previous_revision" || true
  else
    echo "no previous release is available for rollback" >&2
  fi
  exit 1
fi

current_project_images=$(project_image_ids)
for old_image in $old_project_images; do
  printf '%s\n' "$current_project_images" | grep -Fqx -- "$old_image" && continue
  if docker image inspect "$old_image" >/dev/null 2>&1; then
    if docker image rm "$old_image" >/dev/null 2>&1; then
      echo "removed old project image: $old_image"
    else
      echo "warning: old project image is still referenced and was kept: $old_image" >&2
    fi
  fi
done

rm -f -- "$archive" || echo "warning: unable to remove uploaded archive: $archive" >&2
echo "production release $version ($revision) is healthy"

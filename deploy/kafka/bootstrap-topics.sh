#!/usr/bin/env sh
set -eu

topics_file=/topics.txt
partitions="${TORGNEXA_KAFKA_TOPIC_PARTITIONS:-1}"
replication_factor="${TORGNEXA_KAFKA_TOPIC_REPLICATION_FACTOR:-1}"

case "$partitions" in
  ''|*[!0-9]*) echo 'TORGNEXA_KAFKA_TOPIC_PARTITIONS must be a positive integer' >&2; exit 1 ;;
esac
case "$replication_factor" in
  ''|*[!0-9]*) echo 'TORGNEXA_KAFKA_TOPIC_REPLICATION_FACTOR must be a positive integer' >&2; exit 1 ;;
esac
[ "$partitions" -ge 1 ] || { echo 'TORGNEXA_KAFKA_TOPIC_PARTITIONS must be at least 1' >&2; exit 1; }
[ "$replication_factor" -ge 1 ] || { echo 'TORGNEXA_KAFKA_TOPIC_REPLICATION_FACTOR must be at least 1' >&2; exit 1; }
[ -r "$topics_file" ] || { echo "missing $topics_file" >&2; exit 1; }

create_topic() {
  /opt/kafka/bin/kafka-topics.sh \
    --bootstrap-server kafka:29092 \
    --create \
    --if-not-exists \
    --topic "$1" \
    --partitions "$partitions" \
    --replication-factor "$replication_factor" \
    >/dev/null
}

while IFS= read -r topic || [ -n "$topic" ]; do
  case "$topic" in
    ''|'#'*) continue ;;
  esac
  create_topic "$topic"
  create_topic "$topic.retry"
  create_topic "$topic.dlq"
done < "$topics_file"

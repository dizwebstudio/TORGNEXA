<?php

declare(strict_types=1);

$mysqli = new mysqli(
    getenv('DB_HOSTNAME') ?: 'db',
    getenv('DB_USERNAME') ?: 'opencart',
    getenv('DB_PASSWORD') ?: 'opencart-demo',
    getenv('DB_DATABASE') ?: 'opencart',
    (int)(getenv('DB_PORT') ?: 3306)
);

if ($mysqli->connect_errno) {
    fwrite(STDERR, "seed: database connection failed\n");
    exit(1);
}

$mysqli->set_charset('utf8mb4');
$sql = file_get_contents('/usr/local/share/torgnexa-opencart-seed.sql');

if ($sql === false || !$mysqli->multi_query($sql)) {
    fwrite(STDERR, "seed: SQL failed: {$mysqli->error}\n");
    exit(1);
}

while ($mysqli->more_results() && $mysqli->next_result()) {
    if ($mysqli->errno) {
        fwrite(STDERR, "seed: SQL failed: {$mysqli->error}\n");
        exit(1);
    }
}

echo "seed: demo catalog and order ready\n";

<?php

declare(strict_types=1);

$root = '/var/www/html/';
$http = rtrim((string)(getenv('OPENCART_HTTP_SERVER') ?: 'http://127.0.0.1:8095/'), '/') . '/';
$dbDriver = addslashes((string)(getenv('DB_DRIVER') ?: 'mysqli'));
$dbHostname = addslashes((string)(getenv('DB_HOSTNAME') ?: 'db'));
$dbUsername = addslashes((string)(getenv('DB_USERNAME') ?: 'opencart'));
$dbPassword = addslashes((string)(getenv('DB_PASSWORD') ?: 'opencart-demo'));
$dbDatabase = addslashes((string)(getenv('DB_DATABASE') ?: 'opencart'));
$dbPrefix = addslashes((string)(getenv('DB_PREFIX') ?: 'oc_'));
$dbPort = addslashes((string)(getenv('DB_PORT') ?: '3306'));

$db = <<<PHP
// DB
define('DB_DRIVER', '{$dbDriver}');
define('DB_HOSTNAME', '{$dbHostname}');
define('DB_USERNAME', '{$dbUsername}');
define('DB_PASSWORD', '{$dbPassword}');
define('DB_DATABASE', '{$dbDatabase}');
define('DB_PREFIX', '{$dbPrefix}');
define('DB_PORT', '{$dbPort}');

// Cache
define('CACHE_ENGINE', 'file');
PHP;

$catalog = <<<PHP
<?php
// APPLICATION
define('APPLICATION', 'Catalog');

// HTTP
define('HTTP_SERVER', '{$http}');

// DIR
define('DIR_OPENCART', '{$root}');
define('DIR_APPLICATION', DIR_OPENCART . 'catalog/');
define('DIR_SYSTEM', DIR_OPENCART . 'system/');
define('DIR_EXTENSION', DIR_OPENCART . 'extension/');
define('DIR_IMAGE', DIR_OPENCART . 'image/');
define('DIR_STORAGE', DIR_SYSTEM . 'storage/');
define('DIR_LANGUAGE', DIR_APPLICATION . 'language/');
define('DIR_TEMPLATE', DIR_APPLICATION . 'view/template/');
define('DIR_CONFIG', DIR_SYSTEM . 'config/');
define('DIR_CACHE', DIR_STORAGE . 'cache/');
define('DIR_DOWNLOAD', DIR_STORAGE . 'download/');
define('DIR_LOGS', DIR_STORAGE . 'logs/');
define('DIR_SESSION', DIR_STORAGE . 'session/');
define('DIR_UPLOAD', DIR_STORAGE . 'upload/');

{$db}
PHP;
$admin = <<<PHP
<?php
// APPLICATION
define('APPLICATION', 'Admin');

// HTTP
define('HTTP_SERVER', '{$http}admin/');
define('HTTP_CATALOG', '{$http}');

// DIR
define('DIR_OPENCART', '{$root}');
define('DIR_APPLICATION', DIR_OPENCART . 'admin/');
define('DIR_SYSTEM', DIR_OPENCART . 'system/');
define('DIR_EXTENSION', DIR_OPENCART . 'extension/');
define('DIR_IMAGE', DIR_OPENCART . 'image/');
define('DIR_STORAGE', DIR_SYSTEM . 'storage/');
define('DIR_CATALOG', DIR_OPENCART . 'catalog/');
define('DIR_LANGUAGE', DIR_APPLICATION . 'language/');
define('DIR_TEMPLATE', DIR_APPLICATION . 'view/template/');
define('DIR_CONFIG', DIR_SYSTEM . 'config/');
define('DIR_CACHE', DIR_STORAGE . 'cache/');
define('DIR_DOWNLOAD', DIR_STORAGE . 'download/');
define('DIR_LOGS', DIR_STORAGE . 'logs/');
define('DIR_SESSION', DIR_STORAGE . 'session/');
define('DIR_UPLOAD', DIR_STORAGE . 'upload/');

{$db}

// OpenCart API
define('OPENCART_SERVER', 'https://www.opencart.com/');
PHP;

if (file_put_contents($root . 'config.php', $catalog) === false || file_put_contents($root . 'admin/config.php', $admin) === false) {
    fwrite(STDERR, "[torgnexa] unable to write OpenCart config files\n");
    exit(1);
}

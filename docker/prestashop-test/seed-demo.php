<?php
declare(strict_types=1);

// This script runs only inside the disposable local PrestaShop test image.
// It creates synthetic products and a least-privilege Webservice API key.

require_once '/var/www/html/config/config.inc.php';
require_once '/var/www/html/init.php';

$apiKey = getenv('TORGNEXA_PRESTASHOP_API_KEY') ?: '0123456789abcdef0123456789abcdef';
$currencyIso = getenv('TORGNEXA_PRESTASHOP_CURRENCY') ?: 'EUR';
$prefix = _DB_PREFIX_;
$langId = (int) Configuration::get('PS_LANG_DEFAULT');
$shopId = (int) Configuration::get('PS_SHOP_DEFAULT');

if (strlen($apiKey) !== 32 || !preg_match('/^[A-Za-z0-9]+$/', $apiKey)) {
    throw new RuntimeException('invalid demo Webservice key');
}

$db = Db::getInstance();
$keyEscaped = pSQL($apiKey);
$accountId = (int) $db->getValue("SELECT id_webservice_account FROM `{$prefix}webservice_account` WHERE `key` = '{$keyEscaped}'");
if ($accountId === 0) {
    $db->insert('webservice_account', [
        'key' => $apiKey,
        'description' => 'TORGNEXA local Webservice smoke key',
        'class_name' => 'WebserviceRequest',
        'is_module' => 0,
        'active' => 1,
    ]);
    $accountId = (int) $db->Insert_ID();
}

$resources = [
    'products' => ['GET', 'PATCH'],
    'combinations' => ['GET', 'PATCH'],
    'stock_availables' => ['GET', 'PATCH'],
    'orders' => ['GET'],
    'order_details' => ['GET'],
    'order_histories' => ['POST'],
];
foreach ($resources as $resource => $methods) {
    foreach ($methods as $method) {
        $exists = (int) $db->getValue(
            "SELECT id_webservice_permission FROM `{$prefix}webservice_permission` " .
            "WHERE resource = '" . pSQL($resource) . "' AND method = '" . pSQL($method) . "' " .
            "AND id_webservice_account = {$accountId}"
        );
        if ($exists === 0) {
            $db->insert('webservice_permission', [
                'resource' => $resource,
                'method' => $method,
                'id_webservice_account' => $accountId,
            ]);
        }
    }
}
if ($shopId > 0 && (int) $db->getValue("SELECT COUNT(*) FROM `{$prefix}webservice_account_shop` WHERE id_webservice_account = {$accountId} AND id_shop = {$shopId}") === 0) {
    $db->insert('webservice_account_shop', ['id_webservice_account' => $accountId, 'id_shop' => $shopId]);
}

if (!Currency::getIdByIsoCode($currencyIso)) {
    $currencyId = (int) Currency::getIdByIsoCode('EUR');
} else {
    $currencyId = (int) Currency::getIdByIsoCode($currencyIso);
}
if ($currencyId > 0 && (int) Configuration::get('PS_CURRENCY_DEFAULT') !== $currencyId) {
    Configuration::updateValue('PS_CURRENCY_DEFAULT', $currencyId);
}

$categoryId = (int) Configuration::get('PS_HOME_CATEGORY');
if ($categoryId < 1) {
    $categoryId = (int) Configuration::get('PS_ROOT_CATEGORY');
}

$products = [
    ['TORGNEXA-PS-COFFEE', 'TORGNEXA Demo Coffee', 'Synthetic PrestaShop smoke product', 1499.90, 24, 'torgnexa-demo-coffee'],
    ['TORGNEXA-PS-TEA', 'TORGNEXA Demo Tea', 'Synthetic PrestaShop smoke product', 799.00, 8, 'torgnexa-demo-tea'],
];
foreach ($products as [$reference, $name, $description, $price, $quantity, $slug]) {
    $productId = (int) $db->getValue("SELECT id_product FROM `{$prefix}product` WHERE reference = '" . pSQL($reference) . "'");
    if ($productId === 0) {
        $product = new Product();
        $product->reference = $reference;
        $product->name = [$langId => $name];
        $product->link_rewrite = [$langId => $slug];
        $product->description = [$langId => $description];
        $product->description_short = [$langId => $description];
        $product->price = (float) $price;
        $product->active = 1;
        $product->visibility = 'both';
        $product->id_category_default = $categoryId;
        $product->id_tax_rules_group = 0;
        $product->add();
        $productId = (int) $product->id;
        if ($categoryId > 0) {
            $product->addToCategories([$categoryId]);
        }
    } else {
        $product = new Product($productId, false, $langId);
    }
    if ($productId > 0) {
        StockAvailable::setQuantity($productId, 0, (int) $quantity, $shopId);
    }
}

// Keep the toggle as the final write as well. Product/stock ObjectModel hooks
// may rebuild the configuration cache during a fresh install; reasserting the
// row here makes a clean bootstrap deterministic.
$webserviceExists = (int) $db->getValue("SELECT id_configuration FROM `{$prefix}configuration` WHERE name = 'PS_WEBSERVICE' AND id_shop IS NULL AND id_shop_group IS NULL");
if ($webserviceExists > 0) {
    $db->update('configuration', ['value' => 1, 'date_upd' => date('Y-m-d H:i:s')], "name = 'PS_WEBSERVICE' AND id_shop IS NULL AND id_shop_group IS NULL");
} else {
    $db->insert('configuration', ['name' => 'PS_WEBSERVICE', 'value' => 1, 'date_add' => date('Y-m-d H:i:s'), 'date_upd' => date('Y-m-d H:i:s')]);
}

file_put_contents('/var/www/html/.torgnexa-prestashop-seeded', gmdate('c') . "\n", LOCK_EX);
echo "[torgnexa] PrestaShop Webservice key and synthetic products ready\n";

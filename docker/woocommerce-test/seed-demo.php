<?php

if (!function_exists('wc_get_product')) {
    fwrite(STDERR, "WooCommerce is not loaded\n");
    exit(1);
}

global $wpdb;

$adminUser = getenv('WORDPRESS_ADMIN_USER') ?: 'admin';
$consumerKey = getenv('WOO_CONSUMER_KEY') ?: '';
$consumerSecret = getenv('WOO_CONSUMER_SECRET') ?: '';
$webhookSecret = getenv('WOO_WEBHOOK_SECRET') ?: '';
$currency = getenv('WOO_STORE_CURRENCY') ?: 'USD';

if ($consumerKey === '' || $consumerSecret === '' || $webhookSecret === '') {
    fwrite(STDERR, "WooCommerce demo credentials are missing\n");
    exit(1);
}

$admin = get_user_by('login', $adminUser);
if (!$admin) {
    fwrite(STDERR, "WordPress admin user was not found\n");
    exit(1);
}

$apiTable = $wpdb->prefix . 'woocommerce_api_keys';
$hashedKey = wc_api_hash($consumerKey);
$keyId = $wpdb->get_var($wpdb->prepare("SELECT key_id FROM {$apiTable} WHERE consumer_key = %s LIMIT 1", $hashedKey));
if (!$keyId) {
    $inserted = $wpdb->insert(
        $apiTable,
        [
            'user_id' => $admin->ID,
            'description' => 'TORGNEXA Docker smoke test',
            'permissions' => 'read_write',
            'consumer_key' => $hashedKey,
            'consumer_secret' => $consumerSecret,
            'truncated_key' => substr($consumerKey, -7),
        ],
        ['%d', '%s', '%s', '%s', '%s', '%s']
    );
    if ($inserted === false) {
        fwrite(STDERR, "Could not create WooCommerce REST API key\n");
        exit(1);
    }
}

update_option('woocommerce_currency', $currency);
update_option('woocommerce_store_postcode', '101000');
update_option('woocommerce_default_country', 'RU');

$products = [
    [
        'sku' => 'TORGNEXA-WOO-COFFEE',
        'name' => 'TORGNEXA Demo Coffee',
        'description' => 'Synthetic product for the WooCommerce connector smoke test.',
        'price' => '1499.90',
        'stock' => 24,
    ],
    [
        'sku' => 'TORGNEXA-WOO-TEA',
        'name' => 'TORGNEXA Demo Tea',
        'description' => 'Synthetic product for the WooCommerce connector smoke test.',
        'price' => '799.00',
        'stock' => 8,
    ],
];

$productIds = [];
foreach ($products as $row) {
    $productId = wc_get_product_id_by_sku($row['sku']);
    $product = $productId ? wc_get_product($productId) : new WC_Product_Simple();
    if (!$product) {
        fwrite(STDERR, "Could not load product {$row['sku']}\n");
        exit(1);
    }
    $product->set_name($row['name']);
    $product->set_sku($row['sku']);
    $product->set_description($row['description']);
    $product->set_status('publish');
    $product->set_catalog_visibility('visible');
    $product->set_regular_price($row['price']);
    $product->set_manage_stock(true);
    $product->set_stock_quantity($row['stock']);
    $product->set_stock_status('instock');
    $productIds[$row['sku']] = $product->save();
}

$shopPage = wc_get_page_id('shop');
if ($shopPage < 1) {
    $shopPage = wp_insert_post([
        'post_title' => 'Shop',
        'post_name' => 'shop',
        'post_content' => '[products limit="12" columns="3"]',
        'post_status' => 'publish',
        'post_type' => 'page',
    ], true);
    if (is_wp_error($shopPage)) {
        fwrite(STDERR, "Could not create shop page\n");
        exit(1);
    }
    update_option('woocommerce_shop_page_id', $shopPage);
}

$existingOrders = wc_get_orders([
    'limit' => 1,
    'return' => 'ids',
    'meta_key' => '_torgnexa_demo_order',
    'meta_value' => '1',
]);
if (!$existingOrders) {
    $order = wc_create_order(['status' => 'processing']);
    if (is_wp_error($order)) {
        fwrite(STDERR, "Could not create demo order\n");
        exit(1);
    }
    $order->add_product(wc_get_product($productIds['TORGNEXA-WOO-COFFEE']), 2);
    $order->set_currency($currency);
    $order->set_payment_method('cod');
    $order->set_payment_method_title('Synthetic demo payment');
    $order->set_address([
        'first_name' => 'Demo',
        'last_name' => 'Customer',
        'email' => 'demo-customer@torgnexa.local',
        'country' => 'RU',
        'city' => 'Moscow',
        'address_1' => 'Synthetic street 1',
        'postcode' => '101000',
    ], 'billing');
    $order->update_meta_data('_torgnexa_demo_order', '1');
    $order->calculate_totals();
    $order->save();
}

echo "WooCommerce synthetic data ready\n";

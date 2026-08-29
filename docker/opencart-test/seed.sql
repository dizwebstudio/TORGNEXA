INSERT IGNORE INTO `oc_product`
  (`product_id`, `master_id`, `model`, `location`, `variant`, `override`, `quantity`, `stock_status_id`, `image`, `manufacturer_id`, `shipping`, `price`, `points`, `tax_class_id`, `date_available`, `weight`, `weight_class_id`, `length`, `width`, `height`, `length_class_id`, `subtract`, `minimum`, `rating`, `sort_order`, `status`, `date_added`, `date_modified`)
VALUES
  (1001, 0, 'DEMO-COFFEE-001', '', '', '', 24, 7, '', 0, 1, 1499.9000, 0, 0, CURDATE(), 0, 1, 0, 0, 0, 1, 1, 1, 0, 0, 1, UTC_TIMESTAMP(), UTC_TIMESTAMP()),
  (1002, 0, 'DEMO-TEA-002', '', '', '', 8, 7, '', 0, 1, 799.0000, 0, 0, CURDATE(), 0, 1, 0, 0, 0, 1, 1, 1, 0, 0, 1, UTC_TIMESTAMP(), UTC_TIMESTAMP());

INSERT IGNORE INTO `oc_product_description`
  (`product_id`, `language_id`, `name`, `description`, `tag`, `meta_title`, `meta_description`, `meta_keyword`)
VALUES
  (1001, 1, 'TORGNEXA Demo Coffee', 'Synthetic demo product for OpenCart bridge smoke tests.', '', 'TORGNEXA Demo Coffee', '', ''),
  (1002, 1, 'TORGNEXA Demo Tea', 'Synthetic demo product for price and inventory checks.', '', 'TORGNEXA Demo Tea', '', '');

INSERT IGNORE INTO `oc_product_to_store` (`product_id`, `store_id`)
VALUES (1001, 0), (1002, 0);

INSERT IGNORE INTO `oc_product_code` (`product_id`, `code`, `value`)
VALUES (1001, 'SKU', 'DEMO-COFFEE-001'), (1002, 'SKU', 'DEMO-TEA-002');

-- The OpenCart Installer stores one row here. It is what makes the catalog
-- bootstrap register extension/torgnexa for controller/model autoloading.
INSERT IGNORE INTO `oc_extension` (`extension`, `type`, `code`)
VALUES ('torgnexa', 'module', 'torgnexa');

INSERT IGNORE INTO `oc_order`
  (`order_id`, `store_id`, `store_name`, `store_url`, `customer_id`, `customer_group_id`, `firstname`, `lastname`, `email`, `telephone`, `payment_method`, `shipping_method`, `total`, `order_status_id`, `language_id`, `language_code`, `currency_id`, `currency_code`, `currency_value`, `date_added`, `date_modified`)
VALUES
  (9001, 0, 'TORGNEXA OpenCart Demo', 'http://127.0.0.1/', 0, 1, 'Demo', 'Customer', 'demo@example.invalid', '', 'Demo', 'Demo', 2298.9000, 1, 1, 'en-gb', 1, 'USD', 1.00000000, UTC_TIMESTAMP(), UTC_TIMESTAMP());

INSERT IGNORE INTO `oc_order_product`
  (`order_id`, `product_id`, `master_id`, `name`, `model`, `quantity`, `price`, `total`, `tax`, `reward`)
VALUES
  (9001, 1001, 0, 'TORGNEXA Demo Coffee', 'DEMO-COFFEE-001', 1, 1499.9000, 1499.9000, 0, 0),
  (9001, 1002, 0, 'TORGNEXA Demo Tea', 'DEMO-TEA-002', 1, 799.0000, 799.0000, 0, 0);

INSERT IGNORE INTO `oc_order_history` (`order_id`, `order_status_id`, `notify`, `comment`, `date_added`)
VALUES (9001, 1, 0, 'Synthetic demo order for TORGNEXA bridge smoke tests.', UTC_TIMESTAMP());

<?php

namespace Opencart\Catalog\Model\Extension\Torgnexa;

/**
 * Store-local implementation of the versioned TORGNEXA bridge contract.
 *
 * The model deliberately projects only catalog, stock and order state. It
 * never returns customer identity, address, payment or shipping details.
 */
class Api extends \Opencart\System\Engine\Model {
	private const MAX_TEXT = 500;

	private function table(string $name): string {
		return DB_PREFIX . $name;
	}

	private function escape(string $value): string {
		return $this->db->escape($value);
	}

	private function languageId(): int {
		$id = (int)$this->config->get('config_language_id');

		if ($id > 0) {
			return $id;
		}

		$query = $this->db->query('SELECT language_id FROM `' . $this->table('language') . '` ORDER BY sort_order, language_id LIMIT 1');

		return isset($query->row['language_id']) ? (int)$query->row['language_id'] : 1;
	}

	private function skuExpression(): string {
		return "COALESCE((SELECT pc.value FROM `" . $this->table('product_code') . "` pc WHERE pc.product_id = p.product_id AND pc.code = 'SKU' ORDER BY pc.product_code_id LIMIT 1), p.model)";
	}

	private function skuMatch(string $sku): string {
		$escaped = $this->escape($sku);

		return "(p.model = '" . $escaped . "' OR EXISTS (SELECT 1 FROM `" . $this->table('product_code') . "` pc WHERE pc.product_id = p.product_id AND pc.code = 'SKU' AND pc.value = '" . $escaped . "'))";
	}

	private function modelValue(string $sku): string {
		// OpenCart 4 keeps the canonical SKU in product_code.value while the
		// legacy model column is limited to 64 characters and is still required.
		preg_match('/^.{0,64}/us', $sku, $matches);

		return $matches[0] ?? '';
	}

	private function replaceSKU(int $productID, string $sku): void {
		$this->db->query("DELETE FROM `" . $this->table('product_code') . "` WHERE product_id = " . $productID . " AND code = 'SKU'");
		$this->db->query("INSERT INTO `" . $this->table('product_code') . "` SET product_id = " . $productID . ", code = 'SKU', value = '" . $this->escape($sku) . "'");
	}

	public function isTokenValid(string $token): bool {
		$token = trim($token);

		if ($token === '' || strlen($token) > 4096 || preg_match('/[\x00-\x1F\x7F]/', $token)) {
			return false;
		}

		$configured = getenv('TORGNEXA_OPENCART_BRIDGE_TOKEN');

		if ($configured === false || trim((string)$configured) === '') {
			$configured = getenv('TORGNEXA_BRIDGE_TOKEN');
		}

		if ($configured !== false && trim((string)$configured) !== '') {
			return hash_equals(hash('sha256', trim((string)$configured)), hash('sha256', $token));
		}

		$hash = strtolower(trim((string)$this->config->get('torgnexa_token_sha256')));

		return (bool)preg_match('/^[a-f0-9]{64}$/', $hash) && hash_equals($hash, hash('sha256', $token));
	}

	public function ensureSchema(): void {
		$this->db->query('CREATE TABLE IF NOT EXISTS `' . $this->table('torgnexa_idempotency') . '` (
			`idempotency_key` varchar(128) NOT NULL,
			`request_hash` char(64) NOT NULL,
			`response_json` mediumtext NOT NULL,
			`response_status` smallint unsigned NOT NULL DEFAULT 200,
			`created_at` datetime NOT NULL,
			PRIMARY KEY (`idempotency_key`)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci');
		$this->db->query('CREATE TABLE IF NOT EXISTS `' . $this->table('torgnexa_variant_meta') . '` (
			`product_id` int unsigned NOT NULL,
			`compare_at` decimal(15,4) DEFAULT NULL,
			`modified_at` datetime NOT NULL,
			PRIMARY KEY (`product_id`)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci');
	}

	public function idempotency(string $key, string $hash): array {
		$this->ensureSchema();
		$query = $this->db->query("SELECT request_hash, response_json, response_status FROM `" . $this->table('torgnexa_idempotency') . "` WHERE idempotency_key = '" . $this->escape($key) . "'");

		if (!$query->num_rows) {
			return ['state' => 'new'];
		}

		$row = $query->row;

		if (!hash_equals((string)$row['request_hash'], $hash)) {
			return ['state' => 'conflict'];
		}

		$body = json_decode((string)$row['response_json'], true);

		return ['state' => 'replay', 'body' => is_array($body) ? $body : ['error' => 'invalid_replay'], 'status' => (int)$row['response_status']];
	}

	public function rememberIdempotency(string $key, string $hash, array $body, int $status): void {
		$json = json_encode($body, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE);
		// Keep the first response for a key.  Replacing it on a duplicate would
		// make concurrent retries observe a different result than the original
		// mutation, which breaks idempotency semantics.
		$this->db->query("INSERT IGNORE INTO `" . $this->table('torgnexa_idempotency') . "` SET idempotency_key = '" . $this->escape($key) . "', request_hash = '" . $this->escape($hash) . "', response_json = '" . $this->escape((string)$json) . "', response_status = " . (int)$status . ", created_at = UTC_TIMESTAMP()");
	}

	private function productRowByID(int $id): ?array {
		if ($id < 1) {
			return null;
		}

		$query = $this->db->query('SELECT p.product_id, ' . $this->skuExpression() . ' AS sku, p.quantity, p.price, p.status, p.date_added, p.date_modified, pd.name, pd.description, COALESCE(m.name, \'\') AS manufacturer, vm.compare_at FROM `' . $this->table('product') . '` p LEFT JOIN `' . $this->table('product_description') . '` pd ON pd.product_id = p.product_id AND pd.language_id = ' . $this->languageId() . ' LEFT JOIN `' . $this->table('manufacturer') . '` m ON m.manufacturer_id = p.manufacturer_id LEFT JOIN `' . $this->table('torgnexa_variant_meta') . '` vm ON vm.product_id = p.product_id WHERE p.product_id = ' . $id . ' LIMIT 1');

		return $query->num_rows ? $query->row : null;
	}

	private function productRowBySKU(string $sku): ?array {
		$query = $this->db->query("SELECT p.product_id, " . $this->skuExpression() . " AS sku, p.quantity, p.price, p.status, p.date_added, p.date_modified, pd.name, pd.description, COALESCE(m.name, '') AS manufacturer, vm.compare_at FROM `" . $this->table('product') . "` p LEFT JOIN `" . $this->table('product_description') . "` pd ON pd.product_id = p.product_id AND pd.language_id = " . $this->languageId() . " LEFT JOIN `" . $this->table('manufacturer') . "` m ON m.manufacturer_id = p.manufacturer_id LEFT JOIN `" . $this->table('torgnexa_variant_meta') . "` vm ON vm.product_id = p.product_id WHERE " . $this->skuMatch($sku) . " ORDER BY p.product_id LIMIT 1");

		return $query->num_rows ? $query->row : null;
	}

	private function productProjection(array $row): array {
		$modified = (string)($row['date_modified'] ?? '');
		if ($modified === '' || str_starts_with($modified, '0000-00-00')) {
			$modified = (string)($row['date_added'] ?? '');
		}

		return [
			'id' => (int)$row['product_id'],
			'sku' => (string)$row['sku'],
			'title' => (string)$row['name'],
			'description' => (string)($row['description'] ?? ''),
			'brand' => (string)($row['manufacturer'] ?? ''),
			'status' => ((int)$row['status'] === 1) ? 'publish' : 'draft',
			'price' => $this->decimal((string)$row['price']),
			'compare_at' => $row['compare_at'] === null ? '' : $this->decimal((string)$row['compare_at']),
			'quantity' => max(0, (int)$row['quantity']),
			'modified_at' => $this->isoTime($modified),
			'variants' => []
		];
	}

	private function decimal(string $value): string {
		$value = trim($value);

		if (!preg_match('/^-?\d+(?:\.\d{1,4})?$/', $value)) {
			return '0.00';
		}

		[$whole, $fraction] = array_pad(explode('.', $value, 2), 2, '');
		$fraction = rtrim($fraction, '0');

		return $fraction === '' ? $whole . '.00' : $whole . '.' . str_pad(substr($fraction, 0, 4), 2, '0');
	}

	private function isoTime(string $value): string {
		try {
			$time = new \DateTimeImmutable($value === '' ? 'now' : $value, new \DateTimeZone('UTC'));
		} catch (\Exception $exception) {
			$time = new \DateTimeImmutable('now', new \DateTimeZone('UTC'));
		}

		return $time->setTimezone(new \DateTimeZone('UTC'))->format('Y-m-d\\TH:i:s\\Z');
	}

	public function getProduct(int $id): ?array {
		$this->ensureSchema();
		$row = $this->productRowByID($id);

		return $row ? $this->productProjection($row) : null;
	}

	public function getProductBySKU(string $sku): ?array {
		$this->ensureSchema();
		$row = $this->productRowBySKU($sku);

		return $row ? $this->productProjection($row) : null;
	}

	public function listProducts(int $page, int $limit): array {
		$this->ensureSchema();
		$offset = ($page - 1) * $limit;
		$count = $this->db->query('SELECT COUNT(*) AS total FROM `' . $this->table('product') . '`')->row['total'] ?? 0;
		$totalPages = max(1, (int)ceil((int)$count / $limit));
		$query = $this->db->query('SELECT p.product_id, ' . $this->skuExpression() . ' AS sku, p.quantity, p.price, p.status, p.date_added, p.date_modified, pd.name, pd.description, COALESCE(m.name, \'\') AS manufacturer, vm.compare_at FROM `' . $this->table('product') . '` p LEFT JOIN `' . $this->table('product_description') . '` pd ON pd.product_id = p.product_id AND pd.language_id = ' . $this->languageId() . ' LEFT JOIN `' . $this->table('manufacturer') . '` m ON m.manufacturer_id = p.manufacturer_id LEFT JOIN `' . $this->table('torgnexa_variant_meta') . '` vm ON vm.product_id = p.product_id ORDER BY p.product_id LIMIT ' . $offset . ', ' . $limit);
		$items = [];

		foreach ($query->rows as $row) {
			$items[] = $this->productProjection($row);
		}

		return ['items' => $items, 'page' => $page, 'total_pages' => $totalPages];
	}

	private function validText(string $value, int $max = self::MAX_TEXT): bool {
		return $value !== '' && trim($value) === $value && strlen($value) <= $max && !preg_match('/[\x00-\x1F\x7F]/', $value);
	}

	private function statusValue(string $status): ?int {
		return match (strtolower(trim($status))) {
			'publish', 'active', 'enabled' => 1,
			// OpenCart has no private/archived product state.  Keep these
			// provider-neutral statuses unpublished rather than exposing a
			// write that cannot be represented by the shop.
			'draft', 'private', 'archived', 'inactive', 'disabled' => 0,
			default => null
		};
	}

	private function setDescription(int $productID, string $title, string $description): void {
		$languageID = $this->languageId();
		$this->db->query("INSERT INTO `" . $this->table('product_description') . "` SET product_id = " . $productID . ", language_id = " . $languageID . ", name = '" . $this->escape($title) . "', description = '" . $this->escape($description) . "', meta_title = '" . $this->escape($title) . "', meta_description = '', meta_keyword = '' ON DUPLICATE KEY UPDATE name = VALUES(name), description = VALUES(description), meta_title = VALUES(meta_title)");
		$this->db->query('INSERT IGNORE INTO `' . $this->table('product_to_store') . '` SET product_id = ' . $productID . ', store_id = 0');
	}

	public function upsertProduct(array $input): ?array {
		$sku = trim((string)($input['sku'] ?? ''));
		$title = trim((string)($input['title'] ?? ''));
		$description = (string)($input['description'] ?? '');
		$status = $this->statusValue((string)($input['status'] ?? ''));
		$id = (int)($input['id'] ?? 0);

		// OpenCart's product_description.name is VARCHAR(255), while its
		// description column is TEXT (65,535 bytes).  Reject values that would
		// otherwise be silently truncated or fail under strict SQL mode.
		if (!$this->validText($sku, 128) || !$this->validText($title, 255) || strlen($description) > 65535 || $status === null || $id < 0) {
			return null;
		}

		$this->ensureSchema();
		$row = $id > 0 ? $this->productRowByID($id) : $this->productRowBySKU($sku);
		$this->db->query('START TRANSACTION');

		try {
			if ($row) {
				$this->db->query("UPDATE `" . $this->table('product') . "` SET model = '" . $this->escape($this->modelValue($sku)) . "', status = " . $status . ", date_modified = UTC_TIMESTAMP() WHERE product_id = " . (int)$row['product_id']);
				$productID = (int)$row['product_id'];
			} else {
				$this->db->query("INSERT INTO `" . $this->table('product') . "` SET master_id = 0, model = '" . $this->escape($this->modelValue($sku)) . "', location = '', variant = '', `override` = '', quantity = 0, stock_status_id = 0, image = '', manufacturer_id = 0, shipping = 1, price = 0, points = 0, tax_class_id = 0, date_available = CURDATE(), weight = 0, weight_class_id = 0, length = 0, width = 0, height = 0, subtract = 1, minimum = 1, rating = 0, status = " . $status . ", sort_order = 0, date_added = UTC_TIMESTAMP(), date_modified = UTC_TIMESTAMP()");
				$productID = (int)$this->db->getLastId();
			}

			$this->setDescription($productID, $title, $description);
			$this->replaceSKU($productID, $sku);
			$this->db->query('COMMIT');

			return $this->getProduct($productID);
		} catch (\Throwable $exception) {
			$this->db->query('ROLLBACK');

			return null;
		}
	}

	private function variantProductID(string $remoteID): int {
		if (!preg_match('/^product:([1-9][0-9]{0,18})$/', trim($remoteID), $matches)) {
			return 0;
		}

		return (int)$matches[1];
	}

	public function getVariant(string $remoteID): ?array {
		$this->ensureSchema();
		$id = $this->variantProductID($remoteID);
		$row = $this->productRowByID($id);

		if (!$row) {
			return null;
		}

		$projection = $this->productProjection($row);

		return ['remote_id' => $remoteID, 'price' => $projection['price'], 'compare_at' => $projection['compare_at'], 'quantity' => $projection['quantity'], 'modified_at' => $projection['modified_at']];
	}

	public function setVariantPrice(array $input): ?array {
		$id = $this->variantProductID((string)($input['remote_id'] ?? ''));
		$price = trim((string)($input['price'] ?? ''));
		$compareAt = trim((string)($input['compare_at'] ?? ''));

		if ($id < 1 || !preg_match('/^\d+(?:\.\d{1,4})?$/', $price) || ($compareAt !== '' && !preg_match('/^\d+(?:\.\d{1,4})?$/', $compareAt))) {
			return null;
		}

		$this->ensureSchema();
		if (!$this->productRowByID($id)) {
			return null;
		}
		$compareSQL = $compareAt === '' ? 'NULL' : "'" . $this->escape($compareAt) . "'";
		$this->db->query('START TRANSACTION');

		try {
			$this->db->query("UPDATE `" . $this->table('product') . "` SET price = '" . $this->escape($price) . "', date_modified = UTC_TIMESTAMP() WHERE product_id = " . $id);
			$this->db->query("INSERT INTO `" . $this->table('torgnexa_variant_meta') . "` SET product_id = " . $id . ", compare_at = " . $compareSQL . ", modified_at = UTC_TIMESTAMP() ON DUPLICATE KEY UPDATE compare_at = VALUES(compare_at), modified_at = VALUES(modified_at)");
			$this->db->query('COMMIT');

			return $this->getVariant('product:' . $id);
		} catch (\Throwable $exception) {
			$this->db->query('ROLLBACK');

			return null;
		}
	}

	public function setVariantInventory(array $input): ?array {
		$id = $this->variantProductID((string)($input['remote_id'] ?? ''));
		$quantity = filter_var($input['quantity'] ?? null, FILTER_VALIDATE_INT, ['options' => ['min_range' => 0]]);

		$this->ensureSchema();
		if ($id < 1 || $quantity === false || !$this->productRowByID($id)) {
			return null;
		}

		$this->db->query("UPDATE `" . $this->table('product') . "` SET quantity = " . (int)$quantity . ", date_modified = UTC_TIMESTAMP() WHERE product_id = " . $id);

		return $this->getVariant('product:' . $id);
	}

	private function orderProjection(array $row): array {
		$items = [];
		$query = $this->db->query('SELECT order_product_id, product_id, quantity FROM `' . $this->table('order_product') . '` WHERE order_id = ' . (int)$row['order_id'] . ' ORDER BY order_product_id');

		foreach ($query->rows as $item) {
			$items[] = ['id' => (int)$item['order_product_id'], 'variant_remote_id' => 'product:' . (int)$item['product_id'], 'quantity' => max(1, (int)$item['quantity'])];
		}

		return ['id' => (int)$row['order_id'], 'external_id' => (string)$row['order_id'], 'status_remote_id' => (string)$row['order_status_id'], 'created_at' => $this->isoTime((string)$row['date_added']), 'updated_at' => $this->isoTime((string)($row['date_modified'] ?: $row['date_added'])), 'items' => $items];
	}

	private function orderRow(int $id): ?array {
		$query = $this->db->query('SELECT order_id, order_status_id, date_added, date_modified FROM `' . $this->table('order') . '` WHERE order_id = ' . $id . ' LIMIT 1');

		return $query->num_rows ? $query->row : null;
	}

	public function getOrder(int $id): ?array {
		$this->ensureSchema();
		$row = $this->orderRow($id);

		return $row ? $this->orderProjection($row) : null;
	}

	public function listOrders(int $page, int $limit): array {
		$this->ensureSchema();
		$offset = ($page - 1) * $limit;
		$count = $this->db->query('SELECT COUNT(*) AS total FROM `' . $this->table('order') . '`')->row['total'] ?? 0;
		$totalPages = max(1, (int)ceil((int)$count / $limit));
		$query = $this->db->query('SELECT order_id, order_status_id, date_added, date_modified FROM `' . $this->table('order') . '` ORDER BY order_id LIMIT ' . $offset . ', ' . $limit);
		$items = [];

		foreach ($query->rows as $row) {
			$items[] = $this->orderProjection($row);
		}

		return ['items' => $items, 'page' => $page, 'total_pages' => $totalPages];
	}

	public function setOrderStatus(array $input): ?array {
		$id = (int)($input['id'] ?? 0);
		$status = (int)($input['status_remote_id'] ?? 0);

		if ($id < 1 || $status < 1 || !$this->orderRow($id)) {
			return null;
		}

		$statusQuery = $this->db->query('SELECT order_status_id FROM `' . $this->table('order_status') . '` WHERE order_status_id = ' . $status . ' LIMIT 1');

		if (!$statusQuery->num_rows) {
			return null;
		}

		$this->db->query('START TRANSACTION');

		try {
			$this->db->query('UPDATE `' . $this->table('order') . '` SET order_status_id = ' . $status . ', date_modified = UTC_TIMESTAMP() WHERE order_id = ' . $id);
			$this->db->query("INSERT INTO `" . $this->table('order_history') . "` SET order_id = " . $id . ", order_status_id = " . $status . ", notify = 0, comment = 'Updated by TORGNEXA', date_added = UTC_TIMESTAMP()");
			$this->db->query('COMMIT');

			return $this->getOrder($id);
		} catch (\Throwable $exception) {
			$this->db->query('ROLLBACK');

			return null;
		}
	}
}

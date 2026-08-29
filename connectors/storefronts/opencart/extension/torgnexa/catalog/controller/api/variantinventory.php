<?php

namespace Opencart\Catalog\Controller\Extension\Torgnexa\Api;

class Variantinventory extends Base {
	public function index(): void {
		if (!$this->guard()) {
			return;
		}

		if ($this->method() !== 'PUT') {
			$this->respond(['error' => 'method_not_allowed'], 400);

			return;
		}

		$input = $this->input();
		$guard = is_array($input) ? $this->idempotency($input) : ['state' => 'invalid'];

		if ($guard['state'] === 'invalid') {
			$this->respond(['error' => 'invalid_request'], 400);

			return;
		}
		if ($guard['state'] === 'conflict') {
			$this->respond(['error' => 'idempotency_conflict'], 409);

			return;
		}
		if ($guard['state'] === 'replay') {
			$this->respond($guard['record']['body'], $guard['record']['status']);

			return;
		}

		$variant = $this->model()->setVariantInventory($input);

		if (!$variant) {
			$this->respond(['error' => 'invalid_variant_inventory'], 422);

			return;
		}

		$this->remember($guard, $variant);
		$this->respond($variant);
	}
}

<?php

namespace Opencart\Catalog\Controller\Extension\Torgnexa\Api;

class Product extends Base {
	public function index(): void {
		if (!$this->guard()) {
			return;
		}

		switch ($this->method()) {
			case 'GET':
				$this->get();
				return;
			case 'POST':
			case 'PUT':
				$this->write();
				return;
			default:
				$this->respond(['error' => 'method_not_allowed'], 400);
		}
	}

	private function get(): void {
		$id = (int)$this->queryString('id');
		$product = $this->model()->getProduct($id);

		if (!$product) {
			$this->respond(['error' => 'not_found'], 404);

			return;
		}

		$this->respond($product);
	}

	private function write(): void {
		$input = $this->input();

		if (!$input) {
			$this->respond(['error' => 'invalid_json'], 400);

			return;
		}

		$guard = $this->idempotency($input);

		if ($guard['state'] === 'invalid') {
			$this->respond(['error' => 'idempotency_key_required'], 400);

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

		$product = $this->model()->upsertProduct($input);

		if (!$product) {
			$this->respond(['error' => 'invalid_product'], 422);

			return;
		}

		$this->remember($guard, $product);
		$this->respond($product);
	}
}

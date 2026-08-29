<?php

namespace Opencart\Catalog\Controller\Extension\Torgnexa\Api;

class Order extends Base {
	public function index(): void {
		if (!$this->guard()) {
			return;
		}

		$id = (int)$this->queryString('id');
		$order = $this->model()->getOrder($id);

		if (!$order) {
			$this->respond(['error' => 'not_found'], 404);

			return;
		}

		$this->respond($order);
	}
}

<?php

namespace Opencart\Catalog\Controller\Extension\Torgnexa\Api;

class Productbysku extends Base {
	public function index(): void {
		if (!$this->guard()) {
			return;
		}

		$sku = $this->queryString('sku');
		$product = $sku === '' ? null : $this->model()->getProductBySKU($sku);

		if (!$product) {
			$this->respond(['error' => 'not_found'], 404);

			return;
		}

		$this->respond($product);
	}
}

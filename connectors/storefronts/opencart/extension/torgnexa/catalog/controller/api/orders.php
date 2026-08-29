<?php

namespace Opencart\Catalog\Controller\Extension\Torgnexa\Api;

class Orders extends Base {
	public function index(): void {
		if (!$this->guard()) {
			return;
		}

		[$page, $limit] = $this->pageLimit();

		if ($page === 0) {
			$this->respond(['error' => 'invalid_pagination'], 400);

			return;
		}

		$this->respond($this->model()->listOrders($page, $limit));
	}
}

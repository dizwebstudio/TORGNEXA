<?php

namespace Opencart\Catalog\Controller\Extension\Torgnexa\Api;

class Health extends Base {
	public function index(): void {
		if (!$this->guard()) {
			return;
		}

		$this->respond(['ok' => true, 'api_version' => 'v1']);
	}
}

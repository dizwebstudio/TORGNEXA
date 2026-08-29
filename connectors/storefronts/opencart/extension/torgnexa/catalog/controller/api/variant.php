<?php

namespace Opencart\Catalog\Controller\Extension\Torgnexa\Api;

class Variant extends Base {
	public function index(): void {
		if (!$this->guard()) {
			return;
		}

		$remoteID = $this->queryString('remote_id');
		$variant = $remoteID === '' ? null : $this->model()->getVariant($remoteID);

		if (!$variant) {
			$this->respond(['error' => 'not_found'], 404);

			return;
		}

		$this->respond($variant);
	}
}

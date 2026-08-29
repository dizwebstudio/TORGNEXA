<?php

namespace Opencart\Catalog\Controller\Extension\Torgnexa\Api;

/**
 * Shared boundary for the TORGNEXA OpenCart bridge controllers.
 *
 * OpenCart 4 routes each operation to a separate controller file. The
 * connector's hyphenated route names are sanitized by OpenCart's Action and
 * Factory classes, therefore variant-price becomes variantprice at dispatch.
 */
abstract class Base extends \Opencart\System\Engine\Controller {
	protected function model(): object {
		$this->load->model('extension/torgnexa/api');

		return $this->model_extension_torgnexa_api;
	}

	protected function authorize(): bool {
		$header = '';

		if (isset($this->request->server['HTTP_AUTHORIZATION'])) {
			$header = (string)$this->request->server['HTTP_AUTHORIZATION'];
		} elseif (isset($_SERVER['HTTP_AUTHORIZATION'])) {
			$header = (string)$_SERVER['HTTP_AUTHORIZATION'];
		}

		if (!preg_match('/^Bearer\s+(\S+)$/i', trim($header), $matches)) {
			return false;
		}

		return $this->model()->isTokenValid($matches[1]);
	}

	protected function respond(array $body, int $status = 200): void {
		$reasons = [
			400 => 'Bad Request',
			401 => 'Unauthorized',
			404 => 'Not Found',
			409 => 'Conflict',
			422 => 'Unprocessable Entity',
			500 => 'Internal Server Error'
		];

		$this->response->addHeader('Content-Type: application/json; charset=utf-8');

		if ($status !== 200) {
			$this->response->addHeader('HTTP/1.1 ' . $status . ' ' . ($reasons[$status] ?? 'Error'));
		}

		$this->response->setOutput((string)json_encode($body, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE));
	}

	protected function guard(): bool {
		if (!$this->authorize()) {
			$this->respond(['error' => 'unauthorized'], 401);

			return false;
		}

		return true;
	}

	protected function input(): ?array {
		$raw = file_get_contents('php://input');

		if (!is_string($raw) || strlen($raw) > 65536) {
			return null;
		}

		$value = json_decode($raw, true);

		return is_array($value) ? $value : null;
	}

	protected function queryString(string $key): string {
		return isset($this->request->get[$key]) ? trim((string)$this->request->get[$key]) : '';
	}

	protected function pageLimit(): array {
		$page = max(1, (int)($this->request->get['page'] ?? 1));
		$limit = (int)($this->request->get['limit'] ?? 50);

		if ($page > 100000 || $limit < 1 || $limit > 100) {
			return [0, 0];
		}

		return [$page, $limit];
	}

	protected function method(): string {
		if (isset($this->request->server['REQUEST_METHOD'])) {
			return strtoupper((string)$this->request->server['REQUEST_METHOD']);
		}

		return strtoupper((string)($_SERVER['REQUEST_METHOD'] ?? 'GET'));
	}

	protected function idempotency(array $input): array {
		$key = isset($input['idempotency_key']) ? trim((string)$input['idempotency_key']) : '';

		if ($key === '' || strlen($key) > 128 || preg_match('/[\x00-\x1F\x7F]/', $key)) {
			return ['state' => 'invalid'];
		}

		$payload = $input;
		unset($payload['idempotency_key']);
		$encoded = json_encode($payload, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_PRESERVE_ZERO_FRACTION);
		$hash = hash('sha256', (string)$encoded);
		$record = $this->model()->idempotency($key, $hash);

		return ['state' => $record['state'], 'key' => $key, 'hash' => $hash, 'record' => $record];
	}

	protected function remember(array $guard, array $body, int $status = 200): void {
		$this->model()->rememberIdempotency($guard['key'], $guard['hash'], $body, $status);
	}
}

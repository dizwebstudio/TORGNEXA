import json
import unittest
from urllib.request import Request

from torgnexa_sdk import APIError, TorgnexaClient


class ClientTests(unittest.TestCase):
    def test_query_auth_and_no_tenant_override(self):
        captured: list[Request] = []

        def transport(request: Request):
            captured.append(request)
            return 200, {}, b'{"items":[]}'

        client = TorgnexaClient("https://api.example.test/api/v1", bearer_token="token", transport=transport)
        response = client.list_products(q="bolt", status="active", limit=10, cursor="v1.abc")
        self.assertEqual(response.status_code, 200)
        self.assertEqual(response.body, {"items": []})
        self.assertIn("q=bolt", captured[0].full_url)
        self.assertNotIn("organization_id", captured[0].full_url)
        self.assertEqual(captured[0].get_header("Authorization"), "Bearer token")

    def test_path_escaping_and_api_error(self):
        captured: list[Request] = []

        def transport(request: Request):
            captured.append(request)
            return 404, {}, json.dumps({"error": "missing"}).encode()

        client = TorgnexaClient("https://api.example.test/api/v1", transport=transport)
        with self.assertRaises(APIError) as ctx:
            client.mark_notification_read("n/1")
        self.assertEqual(ctx.exception.status_code, 404)
        self.assertIn("n%2F1", captured[0].full_url)

    def test_required_path_parameter_rejected_before_transport(self):
        client = TorgnexaClient("https://api.example.test/api/v1", transport=lambda request: self.fail("transport called"))
        with self.assertRaises(ValueError):
            client.get_notification_preference("")

    def test_binary_pdf_response_is_not_utf8_decoded(self):
        payload = b"%PDF-1.7\n\xff\x00\n%%EOF"

        def transport(request: Request):
            return 200, {"Content-Type": "application/pdf"}, payload

        client = TorgnexaClient("https://api.example.test/api/v1", transport=transport)
        response = client.get_report_data("sales_daily", format="pdf")
        self.assertEqual(response.body, payload)


if __name__ == "__main__":
    unittest.main()

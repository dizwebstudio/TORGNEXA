from torgnexa_sdk import TorgnexaClient

client = TorgnexaClient(
    "https://merchant.example/api/v1",
    bearer_token="replace-with-service-token",
)
response = client.list_products(q="drill", limit=20)
print(response.status_code, response.body)

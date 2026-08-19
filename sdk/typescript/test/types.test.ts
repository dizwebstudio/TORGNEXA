import { TorgnexaClient, type ListProductsRequest } from "@torgnexa/sdk";

const client = new TorgnexaClient({baseURL: "https://api.example.test/api/v1", bearerToken: "token"});
const request: ListProductsRequest = {q: "bolt", limit: 20};
void client.listProducts(request);
void client.getLineageTimeline({system: "catalog", entityType: "product", entityId: "p1"});
void client.createWebhookSubscription({body: {endpoint: "https://hooks.example.test/torgnexa"}});

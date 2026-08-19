import { TorgnexaClient } from "../../typescript/src/client.gen.mjs";

const client = new TorgnexaClient({
  baseURL: "https://merchant.example/api/v1",
  bearerToken: "replace-with-service-token",
});
const response = await client.listProducts({q: "drill", limit: 20});
console.log(response.statusCode, response.body);

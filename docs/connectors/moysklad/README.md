# MoySklad Connector

Task 016 provides the read-only ERP reference connector for MoySklad JSON API 1.2.

Baseline surfaces:
- products: `GET /entity/assortment` with `groupBy=product`;
- inventory: `GET /report/stock/bystore` with `groupBy=product`;
- customer orders: `GET /entity/customerorder`.

The provider authenticates with a bearer token obtained only through the Task-021 secret boundary, requests gzip, and uses the host-injected connector transport. It has no direct network, SQL, filesystem, process, Core, or App authority.

Official references:
- https://dev.moysklad.ru/doc/api/remap/1.2/
- https://github.com/moysklad/api-remap-1.2-doc
- https://github.com/moysklad/php-remap-1.2-sdk

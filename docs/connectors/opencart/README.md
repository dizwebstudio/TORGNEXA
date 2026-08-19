# OpenCart connector

Task 096 adds OpenCart through a narrow versioned TORGNEXA bridge extension contract. This avoids scraping the admin UI, storing OpenCart DB credentials in TORGNEXA, or coupling the Core to OpenCart internals.

Supported by bridge v1: products read/write, prices read/write, inventory read/write, orders read and order status writes.

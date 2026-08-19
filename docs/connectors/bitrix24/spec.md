# Bitrix24 CRM connector spec

Bitrix24 is a `crm` family provider using the universal REST API. The connector reads and writes leads, deals, contacts and companies through `crm.item.*`, and reads/replaces lead/deal product rows through `crm.item.productrow.*`.

Configuration contains only a lower-case portal host such as `acme.bitrix24.com`. Credentials are OAuth2 access-token JSON resolved through `SecretAccessor`; the token is sent in `Authorization: Bearer` and is not appended to the URL or request JSON.

Remote system entity type IDs are lead=1, deal=2, contact=3 and company=4. Product-row owner codes are `L` and `D`. Bitrix24 returns list pages with a fixed size of 50; the opaque TORGNEXA cursor stores remote start plus intra-page offset.

Entity create/update is desired-state oriented. A create first reconciles by `originatorId=TORGNEXA` and `originId`. Updates reconcile by remote ID. Ambiguous writes are read back before success and otherwise fail closed.

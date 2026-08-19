# CIAN property XML mapping

Task 039 emits CIAN Feed v2 with one or more `<Object>` entries. It intentionally qualifies only ordinary apartment sale and long-term/few-month apartment rent; category-specific new-building, suburban and commercial fields require separate admission work.

| TORGNEXA adapter field | CIAN XML | Baseline rule |
|---|---|---|
| `Category` | `Category` | `flatSale` or `flatRent` only |
| `ExternalID` | `ExternalId` | required; unique inside the generated feed |
| `Description` | `Description` | 15..3000 Unicode chars; `&` rejected before XML generation |
| `Address` | `Address` | required exact-address text |
| `Latitude/Longitude` | `Coordinates/Lat,Lng` | optional pair; numeric world bounds |
| `RoomsCount` | `FlatRoomsCount` | 1..7 or 9 |
| `TotalArea` | `TotalArea` | required positive; living + kitchen must be less than total |
| `LivingArea` | `LivingArea` | optional non-negative |
| `KitchenArea` | `KitchenArea` | optional non-negative |
| `FloorNumber` | `FloorNumber` | basement values -1/-2 supported; cannot exceed building floors |
| `FloorsCount` | `Building/FloorsCount` | required positive |
| `Phones` | `Phones/PhoneSchema` | max 2; Task-039 profile accepts +7 plus 10 digits |
| `Photos` | `Photos/PhotoSchema` | max 50; HTTPS-only; at most one default |
| `Price` | `BargainTerms/Price` | required positive integer |
| `Currency` | `BargainTerms/Currency` | `rur`, `usd`, `eur` |
| `MortgageAllowed` | `BargainTerms/MortgageAllowed` | sale only |
| `SaleType` | `BargainTerms/SaleType` | `free` (default) or `alternative`; sale only |
| `LeaseTermType` | `BargainTerms/LeaseTermType` | `longTerm` (default) or `fewMonths`; rent only |

Generated feed constraints are 1..10,000 objects and <=32 MiB. The helper validates duplicate `ExternalId` values and encodes UTF-8 XML with `Feed_Version=2`.

CIAN's category schemas contain many optional and account/product-specific fields. Their omission from this bounded baseline is intentional; callers requiring them must extend the provider mapper with fixtures and schema qualification rather than inject arbitrary XML.

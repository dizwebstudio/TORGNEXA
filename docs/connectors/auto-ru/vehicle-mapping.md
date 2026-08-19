# Auto.ru vehicle XML mapping

Task 038 supports the official passenger-car XML envelope `data/cars/car` and the `modification_id` catalog path. The alternative five engine/transmission parameters are intentionally not admitted in this first profile.

| TORGNEXA adapter field | Auto.ru XML | Baseline rule |
|---|---|---|
| `MarkID` | `mark_id` | required |
| `FolderID` | `folder_id` | required |
| `ModificationID` | `modification_id` | required in Task 038 profile |
| `ComplectationName` | `complectation_name` | NEW required unless `NoComplectation` is explicitly set |
| `BodyType` | `body_type` | required |
| `Wheel` | `wheel` | `Левый` or `Правый` |
| `Color` | `color` | required bounded text |
| `Availability` | `availability` | `В наличии` or `На заказ` |
| `Custom` | `custom` | customs status enumeration |
| `State` | `state` | USED condition enumeration |
| `OwnersNumber` | `owners_number` | USED owner-count enumeration; NEW must be `Не было владельцев` |
| `Run` | `run` | USED positive mileage |
| `Year` | `year` | required, bounded year |
| `RegistryYear` | `registry_year` | USED required; NEW optional when known |
| `DoorsCount` | `doors_count` | 1..8 |
| `Currency` | `currency` | `RUR`, `EUR` or `USD` |
| `VIN` | `vin` | normalized 17-character VIN when present |
| `UniqueID` | `unique_id` | required when VIN absent; letters/digits/`_`/`-`, max 50 |
| `Price` | `price` | integer > 1500 |
| `Description` | `description` | valid UTF-8, max 30,000 characters |
| `Images` | `images/image` | max 40; HTTPS-only TORGNEXA policy |
| `Action` | `action` | optional `hide`/`show` |

The feed is encoded as UTF-8 with lowercase XML element names. The mapper rejects unsafe identifiers, control characters, non-HTTPS images, invalid VINs, over-limit descriptions/images and section-specific field violations before any remote publication is attempted.

// Package searchrepo implements the PostgreSQL MVP adapter for TORGNEXA search.
package searchrepo

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/search"
)

const applyScope = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`

const productSearchSQL = `
WITH search_input AS (
  SELECT CASE WHEN $3='' THEN NULL::tsquery ELSE websearch_to_tsquery('simple'::regconfig,$3) END AS tsq
), ranked AS (
	SELECT p.id,p.code,p.title,p.description,p.status,p.updated_at,COALESCE(img.url,'') AS image_url,
	  regular_price.minor_units,regular_price.currency,
    CASE
      WHEN $3='' THEN 2
      WHEN lower(p.code)=lower($3) OR EXISTS (
        SELECT 1 FROM offers e
        WHERE e.organization_id=$1 AND e.workspace_id=$2 AND e.product_id=p.id
          AND (lower(e.sku)=lower($3) OR lower(COALESCE(e.gtin,''))=lower($3))
      ) THEN 0
      WHEN lower(p.code) LIKE lower($9) ESCAPE E'\\' OR lower(p.title) LIKE lower($9) ESCAPE E'\\' OR EXISTS (
        SELECT 1 FROM offers e
        WHERE e.organization_id=$1 AND e.workspace_id=$2 AND e.product_id=p.id
          AND (lower(e.sku) LIKE lower($9) ESCAPE E'\\' OR lower(COALESCE(e.gtin,'')) LIKE lower($9) ESCAPE E'\\')
      ) THEN 1
      ELSE 2
    END AS priority
	FROM products p CROSS JOIN search_input s
	LEFT JOIN LATERAL (
	  SELECT c.url
	  FROM catalog_product_images c
	  WHERE c.organization_id=p.organization_id AND c.workspace_id=p.workspace_id AND c.product_id=p.id
	  ORDER BY c.position,c.id
	  LIMIT 1
	) img ON true
	LEFT JOIN LATERAL (
	  SELECT pr.minor_units,pr.currency
	  FROM prices pr
	  JOIN offers e ON e.organization_id=pr.organization_id AND e.workspace_id=pr.workspace_id AND e.id=pr.offer_id
	  WHERE pr.organization_id=$1 AND pr.workspace_id=$2 AND e.organization_id=$1 AND e.workspace_id=$2 AND e.product_id=p.id
	    AND e.status='active' AND pr.kind='regular'
	  ORDER BY e.sku,pr.currency,pr.updated_at DESC,pr.id DESC
	  LIMIT 1
	) regular_price ON true
  WHERE p.organization_id=$1 AND p.workspace_id=$2
    AND (p.code NOT LIKE 'DEMO-PRODUCT%' OR NOT EXISTS (SELECT 1 FROM demo_dataset_tombstones d WHERE d.organization_id=$1 AND d.workspace_id=$2))
    AND ($4='' OR p.status=$4)
    AND (
      $3='' OR lower(p.code)=lower($3)
      OR lower(p.code) LIKE lower($9) ESCAPE E'\\'
      OR lower(p.title) LIKE lower($9) ESCAPE E'\\'
      OR search_product_vector(p.code,p.title,p.description) @@ s.tsq
      OR EXISTS (
        SELECT 1 FROM offers e
        WHERE e.organization_id=$1 AND e.workspace_id=$2 AND e.product_id=p.id
          AND (
            lower(e.sku)=lower($3)
            OR lower(COALESCE(e.gtin,''))=lower($3)
            OR lower(e.sku) LIKE lower($9) ESCAPE E'\\'
            OR lower(COALESCE(e.gtin,'')) LIKE lower($9) ESCAPE E'\\'
            OR search_offer_vector(e.sku,e.gtin) @@ s.tsq
          )
      )
    )
)
SELECT id,code,title,description,status,updated_at,image_url,minor_units,currency,priority
FROM ranked
WHERE $5='' OR priority>$6 OR (priority=$6 AND (updated_at<$7 OR (updated_at=$7 AND id<$5)))
ORDER BY priority ASC,updated_at DESC,id DESC
LIMIT $8`

const orderSearchSQL = `
WITH search_input AS (
  SELECT CASE WHEN $3='' THEN NULL::tsquery ELSE websearch_to_tsquery('simple'::regconfig,$3) END AS tsq
), ranked AS (
	SELECT o.id,o.order_number,o.status,o.currency,o.grand_minor_units,o.placed_at,o.updated_at,
	  COALESCE(preview.product_title,'') AS product_title,
	  COALESCE(preview.product_sku,'') AS product_sku,
	  COALESCE(preview.product_image_url,'') AS product_image_url,
	  CASE
      WHEN $3='' THEN 2
      WHEN lower(o.order_number)=lower($3) OR EXISTS (
        SELECT 1 FROM order_items i
        WHERE i.organization_id=$1 AND i.workspace_id=$2 AND i.order_id=o.id
          AND lower(i.sku_snapshot)=lower($3)
      ) THEN 0
      WHEN lower(o.order_number) LIKE lower($11) ESCAPE E'\\' OR EXISTS (
        SELECT 1 FROM order_items i
        WHERE i.organization_id=$1 AND i.workspace_id=$2 AND i.order_id=o.id
          AND lower(i.sku_snapshot) LIKE lower($11) ESCAPE E'\\'
      ) THEN 1
      ELSE 2
    END AS priority
	FROM orders o CROSS JOIN search_input s
	LEFT JOIN LATERAL (
	  SELECT p.title AS product_title,i.sku_snapshot AS product_sku,COALESCE(img.url,'') AS product_image_url
	  FROM order_items i
	  JOIN offers e ON e.organization_id=i.organization_id AND e.workspace_id=i.workspace_id AND e.id=i.offer_id
	  JOIN products p ON p.organization_id=e.organization_id AND p.workspace_id=e.workspace_id AND p.id=e.product_id
	  LEFT JOIN LATERAL (
	    SELECT c.url
	    FROM catalog_product_images c
	    WHERE c.organization_id=p.organization_id AND c.workspace_id=p.workspace_id AND c.product_id=p.id
	    ORDER BY c.position,c.id
	    LIMIT 1
	  ) img ON true
	  WHERE i.organization_id=$1 AND i.workspace_id=$2 AND i.order_id=o.id
	  ORDER BY i.position,i.id
	  LIMIT 1
	) preview ON true
	WHERE o.organization_id=$1 AND o.workspace_id=$2
    AND (o.order_number NOT IN ('DEMO-001','DEMO-002','DEMO-003','DEMO-004','DEMO-005') OR NOT EXISTS (SELECT 1 FROM demo_dataset_tombstones d WHERE d.organization_id=$1 AND d.workspace_id=$2))
    AND ($4='' OR o.status=$4)
    AND ($8::timestamptz IS NULL OR o.placed_at >= $8)
    AND ($9::timestamptz IS NULL OR o.placed_at < $9)
    AND (
      $3='' OR lower(o.order_number)=lower($3)
      OR lower(o.order_number) LIKE lower($11) ESCAPE E'\\'
      OR search_order_vector(o.order_number) @@ s.tsq
      OR EXISTS (
        SELECT 1 FROM order_items i
        WHERE i.organization_id=$1 AND i.workspace_id=$2 AND i.order_id=o.id
          AND (
            lower(i.sku_snapshot)=lower($3)
            OR lower(i.sku_snapshot) LIKE lower($11) ESCAPE E'\\'
            OR search_order_item_vector(i.sku_snapshot) @@ s.tsq
          )
      )
    )
)
SELECT id,order_number,status,currency,grand_minor_units,placed_at,updated_at,product_title,product_sku,product_image_url,priority
FROM ranked
WHERE $5='' OR priority>$6 OR (priority=$6 AND (updated_at<$7 OR (updated_at=$7 AND id<$5)))
ORDER BY priority ASC,updated_at DESC,id DESC
LIMIT $10`

type Repository struct{ db *sql.DB }

type demoCatalogProduct struct {
	Code        string
	Title       string
	Description string
	ImageURL    string
	ImageAlt    string
}

// demoCatalogProducts is intentionally provider-neutral. The remote images
// are public Unsplash CDN URLs so a fresh workspace has a useful visual
// catalog without copying third-party binaries into the repository.
var demoCatalogProducts = []demoCatalogProduct{
	{"DEMO-PRODUCT", "Умные наушники AirBeat X5", "Беспроводные наушники с активным шумоподавлением, прозрачным режимом и автономностью до 32 часов. Подходят для работы, дороги и музыки каждый день.", "https://images.unsplash.com/photo-1578517580179-b517dc10c833?auto=format&fit=crop&w=900&q=80", "Чёрные беспроводные наушники на столе"},
	{"DEMO-PRODUCT-001", "Портативная колонка SoundGo Mini", "Компактная Bluetooth-колонка с чистым вокалом, защитой от брызг и ремешком для переноски. Заряда хватает на целый день прогулок.", "https://images.unsplash.com/photo-1548611716-f156c633d514?auto=format&fit=crop&w=900&q=80", "Портативная колонка и рабочие аксессуары"},
	{"DEMO-PRODUCT-002", "Камера Pocket Snap", "Лёгкая цифровая камера для быстрых кадров в поездках и на прогулках. Автофокус и вспышка помогают получить хороший снимок без долгих настроек.", "https://images.unsplash.com/photo-1526170375885-4d8ecf77b99f?auto=format&fit=crop&w=900&q=80", "Компактная камера на столе"},
	{"DEMO-PRODUCT-003", "Клавиатура Keyline 75", "Механическая клавиатура компактного формата с тихими переключателями, подсветкой и съёмным кабелем USB-C. Удобна для офиса и домашнего рабочего места.", "https://images.unsplash.com/photo-1527443224154-c4a3942d3acf?auto=format&fit=crop&w=900&q=80", "Клавиатура на рабочем столе"},
	{"DEMO-PRODUCT-004", "Пауэрбанк Volt 10 000", "Тонкий внешний аккумулятор на 10 000 мА·ч с двумя выходами и индикатором заряда. Помещается в карман и поддерживает повседневные поездки.", "https://images.unsplash.com/photo-1609592424521-7c2f0f3c1c0b?auto=format&fit=crop&w=900&q=80", "Внешний аккумулятор и кабель"},
	{"DEMO-PRODUCT-005", "Рюкзак Urban Daypack", "Городской рюкзак с отделением для ноутбука 15,6 дюйма, мягкой спинкой и карманом быстрого доступа. Объём 18 литров подходит для работы и коротких поездок.", "https://images.unsplash.com/photo-1553062407-98eeb64c6a62?auto=format&fit=crop&w=900&q=80", "Чёрный городской рюкзак"},
	{"DEMO-PRODUCT-006", "Кроссовки City Run", "Универсальные кроссовки с амортизирующей подошвой и дышащим верхом. Рассчитаны на прогулки по городу, лёгкие пробежки и активный день.", "https://images.unsplash.com/photo-1542291026-7eec264c27ff?auto=format&fit=crop&w=900&q=80", "Красные спортивные кроссовки"},
	{"DEMO-PRODUCT-007", "Смарт-часы Pulse S2", "Часы с мониторингом активности, уведомлениями смартфона и ярким дисплеем. Корпус защищён от брызг, ремешок легко заменить под свой стиль.", "https://images.unsplash.com/photo-1523275335684-37898b6baf30?auto=format&fit=crop&w=900&q=80", "Наручные часы на светлом фоне"},
	{"DEMO-PRODUCT-008", "Очки Northline Classic", "Солнцезащитные очки в универсальной оправе с поляризационными линзами. Лаконичная форма подходит для города, путешествий и отдыха на природе.", "https://images.unsplash.com/photo-1511499767150-a48a237f0083?auto=format&fit=crop&w=900&q=80", "Солнцезащитные очки"},
	{"DEMO-PRODUCT-009", "Термобутылка Steel 750", "Стальная бутылка объёмом 750 мл сохраняет температуру напитка и не впитывает запахи. Герметичная крышка подходит для офиса, спорта и дороги.", "https://images.unsplash.com/photo-1523362628745-0c100150b504?auto=format&fit=crop&w=900&q=80", "Многоразовая металлическая бутылка"},
	{"DEMO-PRODUCT-010", "Чашка Mono Ceramic", "Керамическая чашка объёмом 350 мл с матовой глазурью и удобной ручкой. Минималистичный дизайн хорошо смотрится дома и в офисной кухне.", "https://images.unsplash.com/photo-1514228742587-6b1558fcf93a?auto=format&fit=crop&w=900&q=80", "Керамическая чашка с кофе"},
	{"DEMO-PRODUCT-011", "Настольная лампа Halo", "Настольная лампа с мягким рассеянным светом и регулировкой наклона. Подходит для чтения, видеозвонков и вечерней работы за столом.", "https://images.unsplash.com/photo-1507473885765-e6ed057f782c?auto=format&fit=crop&w=900&q=80", "Настольная лампа в интерьере"},
	{"DEMO-PRODUCT-012", "Мышь Flow Silent", "Беспроводная мышь с бесшумными кнопками, точным сенсором и двумя режимами подключения. Эргономичная форма рассчитана на долгую работу.", "https://images.unsplash.com/photo-1527814050087-3793815479db?auto=format&fit=crop&w=900&q=80", "Компьютерная мышь на столе"},
	{"DEMO-PRODUCT-013", "Ежедневник Paper Grid", "Недатированный ежедневник в твёрдой обложке: планирование недели, заметки и трекер привычек. Плотная бумага подходит для ручки и карандаша.", "https://images.unsplash.com/photo-1455390582262-044cdead277a?auto=format&fit=crop&w=900&q=80", "Открытый блокнот и ручка"},
	{"DEMO-PRODUCT-014", "Свеча Cedar & Amber", "Ароматическая свеча в стеклянном стакане с нотами кедра, янтаря и сухих трав. Даёт тёплый ненавязчивый аромат для домашнего вечера.", "https://images.unsplash.com/photo-1603006905003-be475563bc59?auto=format&fit=crop&w=900&q=80", "Ароматическая свеча"},
	{"DEMO-PRODUCT-015", "Крем для рук Soft Repair", "Увлажняющий крем для рук с лёгкой текстурой и нейтральным ароматом. Быстро впитывается, смягчает кожу и удобен для ежедневного ухода.", "https://images.unsplash.com/photo-1556228578-8c89e6adf883?auto=format&fit=crop&w=900&q=80", "Флакон косметического средства"},
	{"DEMO-PRODUCT-016", "Парфюмерный набор Weekend", "Набор из двух мини-флаконов для поездок и знакомства с ароматами коллекции. Компактный формат удобно брать в ручную кладь или сумку.", "https://images.unsplash.com/photo-1547887538-e3a2f32cb1cc?auto=format&fit=crop&w=900&q=80", "Флакон парфюма"},
	{"DEMO-PRODUCT-017", "Футболка Basic Cotton", "Базовая футболка из мягкого хлопка плотностью 180 г/м². Прямой крой и спокойный оттенок легко сочетаются с повседневным гардеробом.", "https://images.unsplash.com/photo-1521572163474-6864f9cf17ab?auto=format&fit=crop&w=900&q=80", "Сложенная хлопковая футболка"},
	{"DEMO-PRODUCT-018", "Кошелёк Slim Card", "Компактный кошелёк для карт и нескольких купюр из износостойкого материала. Тонкий профиль не создаёт лишнего объёма в кармане.", "https://images.unsplash.com/photo-1627123424574-724758594e93?auto=format&fit=crop&w=900&q=80", "Кожаный кошелёк"},
	{"DEMO-PRODUCT-019", "Камера Lens One", "Зеркальная камера для тех, кто хочет больше контроля над светом и глубиной резкости. Подходит для предметной съёмки, портретов и путешествий.", "https://images.unsplash.com/photo-1516035069371-29a1b244cc32?auto=format&fit=crop&w=900&q=80", "Фотокамера с объективом"},
	{"DEMO-PRODUCT-020", "Растение Mini Green", "Неприхотливое комнатное растение в керамическом кашпо. Компактный размер подходит для рабочего стола, полки или небольшого подоконника.", "https://images.unsplash.com/photo-1497250681960-ef046c08a56e?auto=format&fit=crop&w=900&q=80", "Зелёное комнатное растение"},
	{"DEMO-PRODUCT-021", "Плед Soft Home", "Мягкий плед с фактурной вязкой для дивана, кресла или поездки за город. Сдержанный цвет легко вписывается в современный интерьер.", "https://images.unsplash.com/photo-1584100936595-c0654b55a2e2?auto=format&fit=crop&w=900&q=80", "Мягкий плед в интерьере"},
	{"DEMO-PRODUCT-022", "Органайзер Desk Tray", "Лоток для кабелей, блокнотов и мелких аксессуаров. Помогает держать рабочую поверхность в порядке и быстро находить нужные вещи.", "https://images.unsplash.com/photo-1544816155-12df9643f363?auto=format&fit=crop&w=900&q=80", "Органайзер с канцелярией"},
	{"DEMO-PRODUCT-023", "Кресло Lounge Fold", "Складное кресло с широкой спинкой и мягким сиденьем. Легко переносится между комнатами, балконом и зоной отдыха.", "https://images.unsplash.com/photo-1503602642458-232111445657?auto=format&fit=crop&w=900&q=80", "Кресло в светлом интерьере"},
}

type demoCatalogStatusProduct struct {
	Code, SKU, Title, Description, ImageURL, ImageAlt, Status string
}

// demoCatalogStatusProducts add one draft and one archived card to the
// visual catalog. They deliberately have no active offers, so they cannot
// accidentally become orderable or reserve stock.
var demoCatalogStatusProducts = []demoCatalogStatusProduct{
	{"DEMO-PRODUCT-STATUS-DRAFT", "DEMO-STATUS-DRAFT", "Статус-пример · Черновик", "Карточка товара в работе: описание и изображение уже подготовлены, но предложение ещё не опубликовано.", "https://images.unsplash.com/photo-1494438639946-1ebd1d20bf85?auto=format&fit=crop&w=900&q=80", "Рабочее место с блокнотом", "draft"},
	{"DEMO-PRODUCT-STATUS-ARCHIVED", "DEMO-STATUS-ARCHIVED", "Статус-пример · Архив", "Архивная карточка для демонстрации истории каталога. Товар больше не предлагается к продаже.", "https://images.unsplash.com/photo-1516035069371-29a1b244cc32?auto=format&fit=crop&w=900&q=80", "Камера с объективом на столе", "archived"},
}

type OrderDetail struct {
	ID                 string            `json:"id"`
	OrderNumber        string            `json:"order_number"`
	Status             string            `json:"status"`
	Currency           string            `json:"currency"`
	SubtotalMinorUnits int64             `json:"subtotal_minor_units"`
	DiscountMinorUnits int64             `json:"discount_minor_units"`
	TaxMinorUnits      int64             `json:"tax_minor_units"`
	ShippingMinorUnits int64             `json:"shipping_minor_units"`
	GrandMinorUnits    int64             `json:"grand_minor_units"`
	PlacedAt           time.Time         `json:"placed_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
	Version            int64             `json:"version"`
	Items              []OrderDetailItem `json:"items"`
	Sources            []OrderSource     `json:"sources"`
}
type OrderDetailItem struct {
	SKU                 string `json:"sku"`
	ProductTitle        string `json:"product_title,omitempty"`
	ProductImageURL     string `json:"product_image_url,omitempty"`
	QuantityCoefficient int64  `json:"quantity_coefficient"`
	QuantityScale       int16  `json:"quantity_scale"`
	Unit                string `json:"unit"`
	UnitPriceMinorUnits int64  `json:"unit_price_minor_units"`
	LineTotalMinorUnits int64  `json:"line_total_minor_units"`
}
type OrderSource struct {
	Provider string `json:"provider"`
	RemoteID string `json:"remote_id"`
}

func (r *Repository) OrderDetail(ctx context.Context, scope tenancy.Scope, id string) (OrderDetail, error) {
	if id == "" {
		return OrderDetail{}, search.ErrInvalid
	}
	var out OrderDetail
	err := r.withReadTx(ctx, scope, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT id,order_number,status,currency,subtotal_minor_units,discount_minor_units,tax_minor_units,shipping_minor_units,grand_minor_units,placed_at,version,updated_at FROM orders WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND (order_number NOT IN ('DEMO-001','DEMO-002','DEMO-003','DEMO-004','DEMO-005') OR NOT EXISTS (SELECT 1 FROM demo_dataset_tombstones d WHERE d.organization_id=$1 AND d.workspace_id=$2))`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id).Scan(&out.ID, &out.OrderNumber, &out.Status, &out.Currency, &out.SubtotalMinorUnits, &out.DiscountMinorUnits, &out.TaxMinorUnits, &out.ShippingMinorUnits, &out.GrandMinorUnits, &out.PlacedAt, &out.Version, &out.UpdatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return sql.ErrNoRows
		}
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT i.sku_snapshot,COALESCE(p.title,''),COALESCE(img.url,''),i.quantity_coefficient,i.quantity_scale,i.unit,i.unit_price_minor_units,i.line_total_minor_units FROM order_items i LEFT JOIN offers e ON e.organization_id=i.organization_id AND e.workspace_id=i.workspace_id AND e.id=i.offer_id LEFT JOIN products p ON p.organization_id=e.organization_id AND p.workspace_id=e.workspace_id AND p.id=e.product_id LEFT JOIN LATERAL (SELECT c.url FROM catalog_product_images c WHERE c.organization_id=p.organization_id AND c.workspace_id=p.workspace_id AND c.product_id=p.id ORDER BY c.position,c.id LIMIT 1) img ON true WHERE i.organization_id=$1 AND i.workspace_id=$2 AND i.order_id=$3 ORDER BY i.position,i.id`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id)
		if err != nil {
			return err
		}
		defer rows.Close()
		out.Items = []OrderDetailItem{}
		for rows.Next() {
			var item OrderDetailItem
			if err := rows.Scan(&item.SKU, &item.ProductTitle, &item.ProductImageURL, &item.QuantityCoefficient, &item.QuantityScale, &item.Unit, &item.UnitPriceMinorUnits, &item.LineTotalMinorUnits); err != nil {
				return err
			}
			out.Items = append(out.Items, item)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		sources, err := tx.QueryContext(ctx, `SELECT a.provider,m.remote_id FROM connector_entity_mappings m JOIN connector_accounts a ON a.organization_id=m.organization_id AND a.workspace_id=m.workspace_id AND a.id=m.connector_account_id WHERE m.organization_id=$1 AND m.workspace_id=$2 AND m.entity_type='order' AND m.local_entity_id=$3 ORDER BY a.provider`, scope.OrganizationID().String(), scope.WorkspaceID().String(), id)
		if err != nil {
			return err
		}
		defer sources.Close()
		out.Sources = []OrderSource{}
		for sources.Next() {
			var source OrderSource
			if err := sources.Scan(&source.Provider, &source.RemoteID); err != nil {
				return err
			}
			out.Sources = append(out.Sources, source)
		}
		return sources.Err()
	})
	out.PlacedAt = out.PlacedAt.UTC()
	out.UpdatedAt = out.UpdatedAt.UTC()
	return out, err
}

// SeedDemoOrders atomically creates a tenant-scoped synthetic catalog and five
// orders and is idempotent. The catalog contains 26 products with public demo
// images and human-readable descriptions, including draft and archived cards.
func (r *Repository) SeedDemoOrders(ctx context.Context, scope tenancy.Scope, recipientID string) (int, error) {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || recipientID == "" || len(recipientID) > 128 {
		return 0, search.ErrInvalid
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return 0, fmt.Errorf("search repository: begin demo seed: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	org, ws := scope.OrganizationID().String(), scope.WorkspaceID().String()
	var appliedOrg, appliedWS string
	if err := tx.QueryRowContext(ctx, applyScope, org, ws).Scan(&appliedOrg, &appliedWS); err != nil {
		return 0, fmt.Errorf("search repository: scope demo seed: %w", err)
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM orders WHERE organization_id=$1 AND workspace_id=$2 AND order_number LIKE 'DEMO-%')`, org, ws).Scan(&exists); err != nil {
		return 0, fmt.Errorf("search repository: check demo seed: %w", err)
	}
	if exists {
		var productID string
		if err := tx.QueryRowContext(ctx, `SELECT p.id FROM products p JOIN offers o ON o.organization_id=p.organization_id AND o.workspace_id=p.workspace_id AND o.product_id=p.id WHERE p.organization_id=$1 AND p.workspace_id=$2 AND p.code='DEMO-PRODUCT' AND o.sku='DEMO-SKU'`, org, ws).Scan(&productID); err != nil {
			return 0, fmt.Errorf("search repository: find demo catalog: %w", err)
		}
		stamp := time.Now().UTC()
		if err := seedDemoCatalog(ctx, tx, org, ws, productID, stamp); err != nil {
			return 0, err
		}
		if err := seedDemoCatalogStatusProducts(ctx, tx, org, ws, stamp); err != nil {
			return 0, err
		}
		if err := seedDemoInventory(ctx, tx, org, ws, stamp); err != nil {
			return 0, err
		}
		if err := seedDemoCompliance(ctx, tx, org, ws, productID, stamp); err != nil {
			return 0, err
		}
		if err := seedDemoNotifications(ctx, tx, org, ws, recipientID, stamp); err != nil {
			return 0, err
		}
		if err := seedDemoOrderStatuses(ctx, tx, org, ws, stamp); err != nil {
			return 0, err
		}
		if err := seedDemoFulfillmentAllocations(ctx, tx, org, ws, stamp); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM demo_dataset_tombstones WHERE organization_id=$1 AND workspace_id=$2`, org, ws); err != nil {
			return 0, fmt.Errorf("search repository: restore demo visibility: %w", err)
		}
		if err = tx.Commit(); err != nil {
			return 0, fmt.Errorf("search repository: commit demo restore: %w", err)
		}
		return 0, nil
	}
	productID, offerID := randomUUIDv7(), randomUUIDv7()
	if productID == "" || offerID == "" {
		return 0, errors.New("search repository: random identifier failed")
	}
	stamp := time.Now().UTC()
	primary := demoCatalogProducts[0]
	if _, err = tx.ExecContext(ctx, `INSERT INTO products(id,organization_id,workspace_id,code,title,description,status,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,'draft',$7,$7)`, productID, org, ws, primary.Code, primary.Title, primary.Description, stamp); err != nil {
		return 0, fmt.Errorf("search repository: insert demo product: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE products SET status='active',version=2,updated_at=$4 WHERE id=$1 AND organization_id=$2 AND workspace_id=$3`, productID, org, ws, stamp); err != nil {
		return 0, fmt.Errorf("search repository: activate demo product: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO offers(id,organization_id,workspace_id,product_id,sku,status,created_at,updated_at) VALUES($1,$2,$3,$4,'DEMO-SKU','draft',$5,$5)`, offerID, org, ws, productID, stamp); err != nil {
		return 0, fmt.Errorf("search repository: insert demo offer: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE offers SET status='active',version=2,updated_at=$4 WHERE id=$1 AND organization_id=$2 AND workspace_id=$3`, offerID, org, ws, stamp); err != nil {
		return 0, fmt.Errorf("search repository: activate demo offer: %w", err)
	}
	if err = seedDemoCatalog(ctx, tx, org, ws, productID, stamp); err != nil {
		return 0, err
	}
	if err = seedDemoCatalogStatusProducts(ctx, tx, org, ws, stamp); err != nil {
		return 0, err
	}
	if err = seedDemoInventory(ctx, tx, org, ws, stamp); err != nil {
		return 0, err
	}
	if err = seedDemoCompliance(ctx, tx, org, ws, productID, stamp); err != nil {
		return 0, err
	}
	if err = seedDemoNotifications(ctx, tx, org, ws, recipientID, stamp); err != nil {
		return 0, err
	}
	amounts := []int64{129900, 459000, 79900, 219900, 349000}
	for index, amount := range amounts {
		orderID, itemID := randomUUIDv7(), randomUUIDv7()
		if orderID == "" || itemID == "" {
			return 0, errors.New("search repository: random identifier failed")
		}
		placed, number := stamp.Add(-time.Duration(index)*6*time.Hour), fmt.Sprintf("DEMO-%03d", index+1)
		if _, err = tx.ExecContext(ctx, `INSERT INTO orders(id,organization_id,workspace_id,order_number,status,currency,subtotal_minor_units,discount_minor_units,tax_minor_units,shipping_minor_units,grand_minor_units,placed_at,created_at,updated_at) VALUES($1,$2,$3,$4,'pending','RUB',$5,0,0,0,$5,$6,$6,$6)`, orderID, org, ws, number, amount, placed); err != nil {
			return 0, fmt.Errorf("search repository: insert demo order: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO order_items(id,organization_id,workspace_id,order_id,position,offer_id,sku_snapshot,quantity_coefficient,quantity_scale,unit,unit_price_minor_units,subtotal_minor_units,discount_minor_units,tax_minor_units,line_total_minor_units,tax_jurisdiction,tax_category,tax_rate_coefficient,tax_rate_scale,price_includes_tax,created_at) VALUES($1,$2,$3,$4,1,$5,'DEMO-SKU',1,0,'PCS',$6,$6,0,0,$6,'RU','zero',0,0,true,$7)`, itemID, org, ws, orderID, offerID, amount, placed); err != nil {
			return 0, fmt.Errorf("search repository: insert demo item: %w", err)
		}
	}
	if err = seedDemoOrderStatuses(ctx, tx, org, ws, stamp); err != nil {
		return 0, err
	}
	if err = seedDemoFulfillmentAllocations(ctx, tx, org, ws, stamp); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("search repository: commit demo seed: %w", err)
	}
	return len(amounts), nil
}

func seedDemoCatalog(ctx context.Context, tx *sql.Tx, org, ws, primaryProductID string, stamp time.Time) error {
	for index, item := range demoCatalogProducts {
		productID := primaryProductID
		if item.Code == demoCatalogProducts[0].Code {
			if _, err := tx.ExecContext(ctx, `UPDATE products SET title=$4,description=$5,version=version+1,updated_at=$6 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND (title<>$4 OR description<>$5)`, org, ws, productID, item.Title, item.Description, stamp); err != nil {
				return fmt.Errorf("search repository: refresh demo product: %w", err)
			}
		} else {
			productID = randomUUIDv7()
			if productID == "" {
				return errors.New("search repository: random demo product identifier failed")
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO products(id,organization_id,workspace_id,code,title,description,status,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,'draft',1,$7,$7) ON CONFLICT(organization_id,workspace_id,code) DO NOTHING`, productID, org, ws, item.Code, item.Title, item.Description, stamp); err != nil {
				return fmt.Errorf("search repository: insert demo product %s: %w", item.Code, err)
			}
			if err := tx.QueryRowContext(ctx, `SELECT id FROM products WHERE organization_id=$1 AND workspace_id=$2 AND code=$3`, org, ws, item.Code).Scan(&productID); err != nil {
				return fmt.Errorf("search repository: find demo product %s: %w", item.Code, err)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE products SET status='active',version=2,updated_at=$4 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND status='draft' AND version=1`, org, ws, productID, stamp); err != nil {
				return fmt.Errorf("search repository: activate demo product %s: %w", item.Code, err)
			}
		}

		imageID := randomUUIDv7()
		if imageID == "" {
			return errors.New("search repository: random demo product image identifier failed")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO catalog_product_images(id,organization_id,workspace_id,product_id,url,alt_text,position) VALUES($1,$2,$3,$4,$5,$6,0) ON CONFLICT(organization_id,workspace_id,product_id,url) DO NOTHING`, imageID, org, ws, productID, item.ImageURL, item.ImageAlt); err != nil {
			return fmt.Errorf("search repository: insert demo product image %s: %w", item.Code, err)
		}
		if err := seedDemoOffer(ctx, tx, org, ws, productID, demoSKU(index), stamp); err != nil {
			return err
		}
	}
	return seedDemoCatalogMerchandising(ctx, tx, org, ws, stamp)
}

func seedDemoCatalogStatusProducts(ctx context.Context, tx *sql.Tx, org, ws string, stamp time.Time) error {
	for _, item := range demoCatalogStatusProducts {
		productID := randomUUIDv7()
		if productID == "" {
			return errors.New("search repository: random demo status product identifier failed")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO products(id,organization_id,workspace_id,code,title,description,status,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,'draft',1,$7,$7) ON CONFLICT(organization_id,workspace_id,code) DO NOTHING`, productID, org, ws, item.Code, item.Title, item.Description, stamp); err != nil {
			return fmt.Errorf("search repository: insert demo status product %s: %w", item.Code, err)
		}
		var productStatus string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM products WHERE organization_id=$1 AND workspace_id=$2 AND code=$3`, org, ws, item.Code).Scan(&productStatus); err != nil {
			return fmt.Errorf("search repository: find demo status product %s: %w", item.Code, err)
		}
		if productStatus != "archived" {
			if _, err := tx.ExecContext(ctx, `UPDATE products SET title=$4,description=$5,version=version+1,updated_at=$6 WHERE organization_id=$1 AND workspace_id=$2 AND code=$3 AND status<>'archived' AND (title<>$4 OR description<>$5)`, org, ws, item.Code, item.Title, item.Description, stamp); err != nil {
				return fmt.Errorf("search repository: refresh demo status product %s: %w", item.Code, err)
			}
			offerID := randomUUIDv7()
			if offerID == "" {
				return errors.New("search repository: random demo status offer identifier failed")
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO offers(id,organization_id,workspace_id,product_id,sku,status,created_at,updated_at) VALUES($1,$2,$3,(SELECT id FROM products WHERE organization_id=$2 AND workspace_id=$3 AND code=$4),$5,'draft',$6,$6) ON CONFLICT(organization_id,workspace_id,sku) DO NOTHING`, offerID, org, ws, item.Code, item.SKU, stamp); err != nil {
				return fmt.Errorf("search repository: insert demo status offer %s: %w", item.SKU, err)
			}
			var offerStatus string
			if err := tx.QueryRowContext(ctx, `SELECT status FROM offers WHERE organization_id=$1 AND workspace_id=$2 AND sku=$3`, org, ws, item.SKU).Scan(&offerStatus); err != nil {
				return fmt.Errorf("search repository: find demo status offer %s: %w", item.SKU, err)
			}
			if item.Status == "archived" && offerStatus != "archived" {
				if _, err := tx.ExecContext(ctx, `UPDATE offers SET status='archived',version=version+1,updated_at=$4 WHERE organization_id=$1 AND workspace_id=$2 AND sku=$3 AND status IN ('draft','active')`, org, ws, item.SKU, stamp); err != nil {
					return fmt.Errorf("search repository: archive demo status offer %s: %w", item.SKU, err)
				}
			}
		}
		if item.Status == "archived" && productStatus != "archived" {
			if _, err := tx.ExecContext(ctx, `UPDATE products SET status='archived',version=version+1,updated_at=$4 WHERE organization_id=$1 AND workspace_id=$2 AND code=$3 AND status IN ('draft','active')`, org, ws, item.Code, stamp); err != nil {
				return fmt.Errorf("search repository: archive demo status product %s: %w", item.Code, err)
			}
		}
		imageID := randomUUIDv7()
		if imageID == "" {
			return errors.New("search repository: random demo status product image identifier failed")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO catalog_product_images(id,organization_id,workspace_id,product_id,url,alt_text,position) VALUES($1,$2,$3,(SELECT id FROM products WHERE organization_id=$2 AND workspace_id=$3 AND code=$4),$5,$6,0) ON CONFLICT(organization_id,workspace_id,product_id,url) DO NOTHING`, imageID, org, ws, item.Code, item.ImageURL, item.ImageAlt); err != nil {
			return fmt.Errorf("search repository: insert demo status product image %s: %w", item.Code, err)
		}
	}
	return nil
}

func demoSKU(index int) string {
	if index == 0 {
		return "DEMO-SKU"
	}
	return fmt.Sprintf("DEMO-SKU-%03d", index)
}

func seedDemoOffer(ctx context.Context, tx *sql.Tx, org, ws, productID, sku string, stamp time.Time) error {
	offerID := randomUUIDv7()
	if offerID == "" {
		return errors.New("search repository: random demo offer identifier failed")
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO offers(id,organization_id,workspace_id,product_id,sku,status,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'draft',$6,$6) ON CONFLICT(organization_id,workspace_id,sku) DO NOTHING`, offerID, org, ws, productID, sku, stamp)
	if err != nil {
		return fmt.Errorf("search repository: insert demo offer %s: %w", sku, err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("search repository: inspect demo offer %s: %w", sku, err)
	}
	if inserted == 1 {
		if _, err := tx.ExecContext(ctx, `UPDATE offers SET status='active',version=2,updated_at=$4 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND status='draft' AND version=1`, org, ws, offerID, stamp); err != nil {
			return fmt.Errorf("search repository: activate demo offer %s: %w", sku, err)
		}
	}
	return nil
}

var demoCatalogCategories = []struct {
	id, code, name string
}{
	{"0198b8d0-0000-7000-8000-000000000101", "DEMO-AUDIO", "Аудио"},
	{"0198b8d0-0000-7000-8000-000000000102", "DEMO-ELECTRONICS", "Электроника"},
	{"0198b8d0-0000-7000-8000-000000000103", "DEMO-ACCESSORIES", "Аксессуары"},
	{"0198b8d0-0000-7000-8000-000000000104", "DEMO-HOME", "Дом и интерьер"},
	{"0198b8d0-0000-7000-8000-000000000105", "DEMO-BEAUTY", "Красота и уход"},
	{"0198b8d0-0000-7000-8000-000000000106", "DEMO-CLOTHING", "Одежда и спорт"},
}

// seedDemoCatalogMerchandising fills the relationships that make the demo
// catalog useful beyond the product list: every active offer gets a price and
// every active product gets one primary category.
func seedDemoCatalogMerchandising(ctx context.Context, tx *sql.Tx, org, ws string, stamp time.Time) error {
	for _, category := range demoCatalogCategories {
		if _, err := tx.ExecContext(ctx, `INSERT INTO pim_categories(id,organization_id,workspace_id,code,name,status,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'draft',1,$6,$6) ON CONFLICT(organization_id,workspace_id,code) DO NOTHING`, category.id, org, ws, category.code, category.name, stamp); err != nil {
			return fmt.Errorf("search repository: insert demo category %s: %w", category.code, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE pim_categories SET status='active',version=version+1,updated_at=$4 WHERE organization_id=$1 AND workspace_id=$2 AND code=$3 AND status='draft'`, org, ws, category.code, stamp); err != nil {
			return fmt.Errorf("search repository: activate demo category %s: %w", category.code, err)
		}
	}
	for index, item := range demoCatalogProducts {
		var productID, offerID, categoryID string
		if err := tx.QueryRowContext(ctx, `SELECT p.id,o.id FROM products p JOIN offers o ON o.organization_id=p.organization_id AND o.workspace_id=p.workspace_id AND o.product_id=p.id WHERE p.organization_id=$1 AND p.workspace_id=$2 AND p.code=$3 AND o.sku=$4`, org, ws, item.Code, demoSKU(index)).Scan(&productID, &offerID); err != nil {
			return fmt.Errorf("search repository: find demo merchandising item %s: %w", item.Code, err)
		}
		category := demoCatalogCategories[index%len(demoCatalogCategories)]
		if err := tx.QueryRowContext(ctx, `SELECT id FROM pim_categories WHERE organization_id=$1 AND workspace_id=$2 AND code=$3`, org, ws, category.code).Scan(&categoryID); err != nil {
			return fmt.Errorf("search repository: find demo category %s: %w", category.code, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO pim_product_categories(organization_id,workspace_id,product_id,category_id,is_primary,source,version,active,created_at,updated_at) VALUES($1,$2,$3,$4,true,'demo.seed',1,true,$5,$5) ON CONFLICT(organization_id,workspace_id,product_id,category_id) DO NOTHING`, org, ws, productID, categoryID, stamp); err != nil {
			return fmt.Errorf("search repository: assign demo category %s: %w", item.Code, err)
		}
		regular := int64(79900 + index*12500)
		if index == 0 {
			regular = 129900
		}
		prices := []struct {
			kind   string
			amount int64
		}{
			{kind: "regular", amount: regular},
		}
		if index%3 == 0 {
			prices = append(prices, struct {
				kind   string
				amount int64
			}{kind: "compare_at", amount: regular + 25000})
		}
		for _, price := range prices {
			priceID := randomUUIDv7()
			if priceID == "" {
				return errors.New("search repository: random demo price identifier failed")
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO prices(id,organization_id,workspace_id,offer_id,kind,minor_units,currency,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,'RUB',1,$7,$7) ON CONFLICT(organization_id,workspace_id,offer_id,kind,currency) DO NOTHING`, priceID, org, ws, offerID, price.kind, price.amount, stamp); err != nil {
				return fmt.Errorf("search repository: insert demo price %s: %w", item.Code, err)
			}
		}
	}
	return nil
}

func seedDemoFulfillmentAllocations(ctx context.Context, tx *sql.Tx, org, ws string, stamp time.Time) error {
	warehouseID := ""
	if err := tx.QueryRowContext(ctx, `SELECT id FROM warehouses WHERE organization_id=$1 AND workspace_id=$2 AND code='DEMO-WAREHOUSE'`, org, ws).Scan(&warehouseID); err != nil {
		return fmt.Errorf("search repository: find demo fulfillment warehouse: %w", err)
	}
	for index, orderNumber := range []string{"DEMO-001", "DEMO-002", "DEMO-003"} {
		allocationID := fmt.Sprintf("0198b8d0-0000-7000-8000-00000000020%d", index+1)
		idempotencyKey := fmt.Sprintf("demo:fulfillment:%03d", index+1)
		if _, err := tx.ExecContext(ctx, `INSERT INTO fulfillment_allocations(organization_id,workspace_id,allocation_id,idempotency_key,order_id,order_item_id,offer_id,warehouse_id,quantity_coefficient,quantity_scale,unit,state,reason_code,version,created_at,updated_at) SELECT $1,$2,$3,$4,o.id,i.id,i.offer_id,$5,i.quantity_coefficient,i.quantity_scale,i.unit,'reserved','demo_order_fulfillment',1,$6,$6 FROM orders o JOIN order_items i ON i.organization_id=o.organization_id AND i.workspace_id=o.workspace_id AND i.order_id=o.id WHERE o.organization_id=$1 AND o.workspace_id=$2 AND o.order_number=$7 ON CONFLICT DO NOTHING`, org, ws, allocationID, idempotencyKey, warehouseID, stamp, orderNumber); err != nil {
			return fmt.Errorf("search repository: insert demo fulfillment allocation %s: %w", orderNumber, err)
		}
	}
	return nil
}

var demoOrderStatusPaths = []struct {
	number string
	path   []string
}{
	{number: "DEMO-002", path: []string{"confirmed"}},
	{number: "DEMO-003", path: []string{"confirmed", "processing"}},
	{number: "DEMO-004", path: []string{"confirmed", "processing", "fulfilled"}},
	{number: "DEMO-005", path: []string{"cancelled"}},
}

func seedDemoOrderStatuses(ctx context.Context, tx *sql.Tx, org, ws string, stamp time.Time) error {
	for _, target := range demoOrderStatusPaths {
		var status string
		var version int64
		if err := tx.QueryRowContext(ctx, `SELECT status,version FROM orders WHERE organization_id=$1 AND workspace_id=$2 AND order_number=$3`, org, ws, target.number).Scan(&status, &version); err != nil {
			return fmt.Errorf("search repository: find demo order %s: %w", target.number, err)
		}
		for _, nextStatus := range target.path {
			if status == nextStatus {
				continue
			}
			if !demoOrderCanAdvance(status, nextStatus) {
				break
			}
			result, err := tx.ExecContext(ctx, `UPDATE orders SET status=$4,version=version+1,updated_at=GREATEST(updated_at,$5) WHERE organization_id=$1 AND workspace_id=$2 AND order_number=$3 AND status=$6 AND version=$7`, org, ws, target.number, nextStatus, stamp, status, version)
			if err != nil {
				return fmt.Errorf("search repository: advance demo order %s to %s: %w", target.number, nextStatus, err)
			}
			updated, err := result.RowsAffected()
			if err != nil {
				return fmt.Errorf("search repository: inspect demo order %s transition: %w", target.number, err)
			}
			if updated != 1 {
				return fmt.Errorf("search repository: demo order %s transition was not applied", target.number)
			}
			status, version = nextStatus, version+1
		}
	}
	return nil
}

func demoOrderCanAdvance(current, next string) bool {
	switch {
	case current == "pending" && (next == "confirmed" || next == "cancelled"):
		return true
	case current == "confirmed" && next == "processing":
		return true
	case current == "processing" && next == "fulfilled":
		return true
	default:
		return false
	}
}

func seedDemoInventory(ctx context.Context, tx *sql.Tx, org, ws string, stamp time.Time) error {
	warehouseID := randomUUIDv7()
	if warehouseID == "" {
		return errors.New("search repository: random demo warehouse identifier failed")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO warehouses(id,organization_id,workspace_id,code,name,status,version,created_at,updated_at) VALUES($1,$2,$3,'DEMO-WAREHOUSE','Демонстрационный склад','active',1,$4,$4) ON CONFLICT(organization_id,workspace_id,code) DO NOTHING`, warehouseID, org, ws, stamp); err != nil {
		return fmt.Errorf("search repository: insert demo warehouse: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT id FROM warehouses WHERE organization_id=$1 AND workspace_id=$2 AND code='DEMO-WAREHOUSE'`, org, ws).Scan(&warehouseID); err != nil {
		return fmt.Errorf("search repository: find demo warehouse: %w", err)
	}
	rows, err := tx.QueryContext(ctx, `SELECT o.id,o.sku FROM offers o JOIN products p ON p.organization_id=o.organization_id AND p.workspace_id=o.workspace_id AND p.id=o.product_id WHERE o.organization_id=$1 AND o.workspace_id=$2 AND p.code LIKE 'DEMO-PRODUCT%' AND p.status='active' AND o.status='active' ORDER BY CASE WHEN o.sku='DEMO-SKU' THEN 0 ELSE 1 END,o.sku`, org, ws)
	if err != nil {
		return fmt.Errorf("search repository: list demo offers for inventory: %w", err)
	}
	type demoOffer struct{ id, sku string }
	offers := make([]demoOffer, 0, len(demoCatalogProducts))
	for rows.Next() {
		var offer demoOffer
		if err := rows.Scan(&offer.id, &offer.sku); err != nil {
			rows.Close()
			return fmt.Errorf("search repository: scan demo offer for inventory: %w", err)
		}
		offers = append(offers, offer)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("search repository: read demo offers for inventory: %w", err)
	}
	rows.Close()
	for index, offer := range offers {
		onHand, reserved := int64(24+index*3), int64(index%4)
		if offer.sku == "DEMO-SKU" {
			onHand, reserved = 48, 7
		}
		if err := seedDemoInventoryPosition(ctx, tx, org, ws, offer.id, warehouseID, onHand, reserved, stamp); err != nil {
			return err
		}
	}
	if len(offers) > 1 {
		if err := seedDemoIncidentScenario(ctx, tx, org, ws, offers[1].id, stamp); err != nil {
			return err
		}
	}
	if err := seedDemoIncidentHistory(ctx, tx, org, ws, stamp); err != nil {
		return err
	}
	return nil
}

func seedDemoInventoryPosition(ctx context.Context, tx *sql.Tx, org, ws, offerID, warehouseID string, onHand, reserved int64, stamp time.Time) error {
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM inventory_positions WHERE organization_id=$1 AND workspace_id=$2 AND offer_id=$3 AND warehouse_id=$4)`, org, ws, offerID, warehouseID).Scan(&exists); err != nil {
		return fmt.Errorf("search repository: inspect demo inventory: %w", err)
	}
	if exists {
		return nil
	}
	positionID := randomUUIDv7()
	if positionID == "" {
		return errors.New("search repository: random demo inventory identifier failed")
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO inventory_positions(id,organization_id,workspace_id,offer_id,warehouse_id,unit,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'PCS',$6,$6) ON CONFLICT(organization_id,workspace_id,offer_id,warehouse_id) DO NOTHING`, positionID, org, ws, offerID, warehouseID, stamp)
	if err != nil {
		return fmt.Errorf("search repository: insert demo inventory: %w", err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("search repository: inspect demo inventory insert: %w", err)
	}
	if inserted == 1 {
		if _, err := tx.ExecContext(ctx, `UPDATE inventory_positions SET on_hand_coefficient=$4,reserved_coefficient=$5,version=2,updated_at=$6 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=1`, org, ws, positionID, onHand, reserved, stamp); err != nil {
			return fmt.Errorf("search repository: stock demo inventory: %w", err)
		}
	}
	return nil
}

func seedDemoIncidentScenario(ctx context.Context, tx *sql.Tx, org, ws, offerID string, stamp time.Time) error {
	warehouseID := randomUUIDv7()
	if warehouseID == "" {
		return errors.New("search repository: random demo incident warehouse identifier failed")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO warehouses(id,organization_id,workspace_id,code,name,status,version,created_at,updated_at) VALUES($1,$2,$3,'DEMO-INCIDENT-WH','Демо-склад для инцидентов','active',1,$4,$4) ON CONFLICT(organization_id,workspace_id,code) DO NOTHING`, warehouseID, org, ws, stamp); err != nil {
		return fmt.Errorf("search repository: insert demo incident warehouse: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT id FROM warehouses WHERE organization_id=$1 AND workspace_id=$2 AND code='DEMO-INCIDENT-WH'`, org, ws).Scan(&warehouseID); err != nil {
		return fmt.Errorf("search repository: find demo incident warehouse: %w", err)
	}
	if err := seedDemoInventoryPosition(ctx, tx, org, ws, offerID, warehouseID, 18, 0, stamp); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO warehouse_operational_state(organization_id,workspace_id,warehouse_id,state,reason_code,version,changed_at) VALUES($1,$2,$3,'unavailable','demo_outage',1,$4) ON CONFLICT(organization_id,workspace_id,warehouse_id) DO NOTHING`, org, ws, warehouseID, stamp); err != nil {
		return fmt.Errorf("search repository: insert demo operational state: %w", err)
	}
	const incidentID = "whinc_00000000000000000000000000000001"
	if _, err := tx.ExecContext(ctx, `INSERT INTO warehouse_incidents(organization_id,workspace_id,incident_id,warehouse_id,operational_state,reason_code,status,opened_at,updated_at) VALUES($1,$2,$3,$4,'unavailable','demo_outage','open',$5,$5) ON CONFLICT(organization_id,workspace_id,incident_id) DO NOTHING`, org, ws, incidentID, warehouseID, stamp); err != nil {
		return fmt.Errorf("search repository: insert demo warehouse incident: %w", err)
	}
	return nil
}

var demoIncidentHistory = []struct {
	warehouseCode, warehouseName, incidentID, operationalState, reasonCode, status string
}{
	{"DEMO-INCIDENT-COMPLETED", "Демо-склад · инцидент завершён", "whinc_00000000000000000000000000000002", "unavailable", "demo_recovered", "completed"},
	{"DEMO-INCIDENT-RESOLVED", "Демо-склад · инцидент решён", "whinc_00000000000000000000000000000003", "lost", "demo_resolved", "resolved"},
}

// seedDemoIncidentHistory keeps terminal incident states visible in the
// inventory history. Open and processing are transient worker states and are
// represented by the live incident scenario above before automation closes it.
func seedDemoIncidentHistory(ctx context.Context, tx *sql.Tx, org, ws string, stamp time.Time) error {
	for _, item := range demoIncidentHistory {
		warehouseID := randomUUIDv7()
		if warehouseID == "" {
			return errors.New("search repository: random demo incident history warehouse identifier failed")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO warehouses(id,organization_id,workspace_id,code,name,status,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'active',1,$6,$6) ON CONFLICT(organization_id,workspace_id,code) DO NOTHING`, warehouseID, org, ws, item.warehouseCode, item.warehouseName, stamp); err != nil {
			return fmt.Errorf("search repository: insert demo incident history warehouse %s: %w", item.warehouseCode, err)
		}
		if err := tx.QueryRowContext(ctx, `SELECT id FROM warehouses WHERE organization_id=$1 AND workspace_id=$2 AND code=$3`, org, ws, item.warehouseCode).Scan(&warehouseID); err != nil {
			return fmt.Errorf("search repository: find demo incident history warehouse %s: %w", item.warehouseCode, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO warehouse_operational_state(organization_id,workspace_id,warehouse_id,state,reason_code,version,changed_at) VALUES($1,$2,$3,$4,$5,1,$6) ON CONFLICT(organization_id,workspace_id,warehouse_id) DO NOTHING`, org, ws, warehouseID, item.operationalState, item.reasonCode, stamp); err != nil {
			return fmt.Errorf("search repository: insert demo incident history state %s: %w", item.warehouseCode, err)
		}
		openedAt, completedAt := stamp.Add(-4*time.Hour), stamp.Add(-3*time.Hour)
		if _, err := tx.ExecContext(ctx, `INSERT INTO warehouse_incidents(organization_id,workspace_id,incident_id,warehouse_id,operational_state,reason_code,status,opened_at,updated_at,completed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(organization_id,workspace_id,incident_id) DO NOTHING`, org, ws, item.incidentID, warehouseID, item.operationalState, item.reasonCode, item.status, openedAt, completedAt, completedAt); err != nil {
			return fmt.Errorf("search repository: insert demo incident history %s: %w", item.status, err)
		}
	}
	return nil
}

func seedDemoCompliance(ctx context.Context, tx *sql.Tx, org, ws, productID string, stamp time.Time) error {
	documentID := randomUUIDv7()
	if documentID == "" {
		return errors.New("search repository: random demo compliance identifier failed")
	}
	issuedAt, expiresAt := stamp.AddDate(0, -1, 0), stamp.AddDate(1, 0, 0)
	result, err := tx.ExecContext(ctx, `INSERT INTO compliance_documents(id,organization_id,workspace_id,document_type,number,jurisdiction,issuer,registry_source,registry_reference,status,issued_at,expires_at,verification_source,verified_at,version,created_at,updated_at) VALUES($1,$2,$3,'declaration','DEMO-EAEU-RU-001','RU','Демонстрационный орган сертификации','demo.registry','DEMO-REGISTRY-001','draft',$4,$5,'',NULL,1,$6,$6) ON CONFLICT(organization_id,workspace_id,jurisdiction,document_type,number) WHERE status<>'revoked' DO NOTHING`, documentID, org, ws, issuedAt, expiresAt, stamp)
	if err != nil {
		return fmt.Errorf("search repository: insert demo compliance document: %w", err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("search repository: inspect demo compliance insert: %w", err)
	}
	if inserted == 1 {
		if _, err := tx.ExecContext(ctx, `UPDATE compliance_documents SET status='valid',verification_source='demo.registry',verified_at=$4,version=2,updated_at=$4 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=1`, org, ws, documentID, stamp); err != nil {
			return fmt.Errorf("search repository: verify demo compliance document: %w", err)
		}
	} else if err := tx.QueryRowContext(ctx, `SELECT id FROM compliance_documents WHERE organization_id=$1 AND workspace_id=$2 AND jurisdiction='RU' AND document_type='declaration' AND number='DEMO-EAEU-RU-001' AND status<>'revoked'`, org, ws).Scan(&documentID); err != nil {
		return fmt.Errorf("search repository: find demo compliance document: %w", err)
	}
	bindingID := randomUUIDv7()
	if bindingID == "" {
		return errors.New("search repository: random demo compliance binding identifier failed")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO compliance_bindings(id,organization_id,workspace_id,document_id,subject_type,subject_id,active,version,created_at,updated_at) VALUES($1,$2,$3,$4,'product',$5,true,1,$6,$6) ON CONFLICT(organization_id,workspace_id,document_id,subject_type,subject_id) DO NOTHING`, bindingID, org, ws, documentID, productID, stamp); err != nil {
		return fmt.Errorf("search repository: bind demo compliance document: %w", err)
	}
	return nil
}

func seedDemoNotifications(ctx context.Context, tx *sql.Tx, org, ws, recipientID string, stamp time.Time) error {
	items := []struct {
		severity, key, title, body string
		offset                     time.Duration
	}{
		{"info", "demo.dataset.ready", "Демонстрационный контур готов", "Созданы 26 товаров, пять заказов с разными статусами, складской остаток и декларация соответствия.", -2 * time.Hour},
		{"warning", "demo.stock.reservation", "Часть остатка зарезервирована", "На демонстрационном складе зарезервировано 7 из 48 единиц товара DEMO-SKU.", -time.Hour},
		{"critical", "demo.compliance.expiry", "Проверьте срок декларации", "Демонстрационное критическое уведомление показывает, как выглядят события, требующие внимания.", 0},
	}
	for _, item := range items {
		id := randomUUIDv7()
		if id == "" {
			return errors.New("search repository: random demo notification identifier failed")
		}
		occurred := stamp.Add(item.offset)
		if _, err := tx.ExecContext(ctx, `INSERT INTO notifications(id,organization_id,workspace_id,recipient_id,dedupe_key,severity,title,body,occurrence_count,first_occurred_at,last_occurred_at,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,1,$9,$9,$9,$9) ON CONFLICT(organization_id,workspace_id,recipient_id,dedupe_key) DO UPDATE SET severity=EXCLUDED.severity,title=EXCLUDED.title,body=EXCLUDED.body,last_occurred_at=GREATEST(notifications.last_occurred_at,EXCLUDED.last_occurred_at),updated_at=GREATEST(notifications.updated_at,EXCLUDED.updated_at)`, id, org, ws, recipientID, item.key, item.severity, item.title, item.body, occurred); err != nil {
			return fmt.Errorf("search repository: insert demo notification: %w", err)
		}
	}
	return nil
}

// DeleteDemoOrders logically removes only synthetic data while retaining the
// immutable canonical order history. It is safe to repeat.
func (r *Repository) DeleteDemoOrders(ctx context.Context, scope tenancy.Scope) (int, error) {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() {
		return 0, search.ErrInvalid
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return 0, fmt.Errorf("search repository: begin demo delete: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	org, ws := scope.OrganizationID().String(), scope.WorkspaceID().String()
	var appliedOrg, appliedWS string
	if err := tx.QueryRowContext(ctx, applyScope, org, ws).Scan(&appliedOrg, &appliedWS); err != nil {
		return 0, fmt.Errorf("search repository: scope demo delete: %w", err)
	}
	var deleted int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM orders WHERE organization_id=$1 AND workspace_id=$2 AND order_number IN ('DEMO-001','DEMO-002','DEMO-003','DEMO-004','DEMO-005') AND NOT EXISTS (SELECT 1 FROM demo_dataset_tombstones d WHERE d.organization_id=$1 AND d.workspace_id=$2)`, org, ws).Scan(&deleted); err != nil {
		return 0, fmt.Errorf("search repository: count demo orders: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO demo_dataset_tombstones(organization_id,workspace_id) VALUES($1,$2) ON CONFLICT(organization_id,workspace_id) DO UPDATE SET deleted_at=clock_timestamp()`, org, ws); err != nil {
		return 0, fmt.Errorf("search repository: hide demo dataset: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("search repository: commit demo delete: %w", err)
	}
	return deleted, nil
}

func randomUUIDv7() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return ""
	}
	millis := uint64(time.Now().UnixMilli())
	value[0], value[1], value[2], value[3], value[4], value[5] = byte(millis>>40), byte(millis>>32), byte(millis>>24), byte(millis>>16), byte(millis>>8), byte(millis)
	value[6], value[8] = (value[6]&0x0f)|0x70, (value[8]&0x3f)|0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16])
}

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("search repository: database required")
	}
	return &Repository{db: db}, nil
}

func (r *Repository) SearchProducts(ctx context.Context, scope tenancy.Scope, query search.ProductQuery) (search.ProductPage, error) {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || query.Validate() != nil {
		return search.ProductPage{}, search.ErrInvalid
	}
	fingerprint := search.ProductFingerprint(query)
	cursor, err := search.ParseCursor(query.Cursor, fingerprint)
	if err != nil {
		return search.ProductPage{}, search.ErrInvalid
	}
	var page search.ProductPage
	err = r.withReadTx(ctx, scope, func(tx *sql.Tx) error {
		cursorAt := cursor.UpdatedAt
		if cursorAt.IsZero() {
			cursorAt = time.Unix(0, 0).UTC()
		}
		rows, err := tx.QueryContext(ctx, productSearchSQL,
			scope.OrganizationID().String(), scope.WorkspaceID().String(), query.Text, query.Status,
			cursor.ID, cursor.Priority, cursorAt, query.Limit+1, likePrefix(query.Text),
		)
		if err != nil {
			return fmt.Errorf("search repository: product query failed: %w", err)
		}
		defer rows.Close()

		type rankedHit struct {
			hit      search.ProductHit
			priority int
		}
		hits := make([]rankedHit, 0, query.Limit+1)
		for rows.Next() {
			var item rankedHit
			var priceMinor sql.NullInt64
			var priceCurrency sql.NullString
			if err := rows.Scan(&item.hit.ID, &item.hit.Code, &item.hit.Title, &item.hit.Description, &item.hit.Status, &item.hit.UpdatedAt, &item.hit.ImageURL, &priceMinor, &priceCurrency, &item.priority); err != nil {
				return fmt.Errorf("search repository: product row failed: %w", err)
			}
			item.hit.UpdatedAt = item.hit.UpdatedAt.UTC()
			if priceMinor.Valid != priceCurrency.Valid {
				return search.ErrInvalid
			}
			if priceMinor.Valid {
				item.hit.Price = &search.ProductPrice{MinorUnits: priceMinor.Int64, Currency: priceCurrency.String}
			}
			if item.priority < 0 || item.priority > 2 || item.hit.Validate() != nil {
				return search.ErrInvalid
			}
			hits = append(hits, item)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("search repository: product rows failed: %w", err)
		}
		if len(hits) > query.Limit {
			last := hits[query.Limit-1]
			next, err := search.NewCursor(last.priority, last.hit.UpdatedAt, last.hit.ID, fingerprint)
			if err != nil {
				return err
			}
			page.NextCursor = next
			hits = hits[:query.Limit]
		}
		page.Items = make([]search.ProductHit, len(hits))
		for i := range hits {
			page.Items[i] = hits[i].hit
		}
		return page.Validate()
	})
	return page, err
}

func (r *Repository) SearchOrders(ctx context.Context, scope tenancy.Scope, query search.OrderQuery) (search.OrderPage, error) {
	if r == nil || r.db == nil || ctx == nil || !scope.Valid() || query.Validate() != nil {
		return search.OrderPage{}, search.ErrInvalid
	}
	fingerprint := search.OrderFingerprint(query)
	cursor, err := search.ParseCursor(query.Cursor, fingerprint)
	if err != nil {
		return search.OrderPage{}, search.ErrInvalid
	}
	var page search.OrderPage
	err = r.withReadTx(ctx, scope, func(tx *sql.Tx) error {
		cursorAt := cursor.UpdatedAt
		if cursorAt.IsZero() {
			cursorAt = time.Unix(0, 0).UTC()
		}
		var placedFrom, placedTo any
		if query.PlacedFrom != nil {
			placedFrom = *query.PlacedFrom
		}
		if query.PlacedTo != nil {
			placedTo = *query.PlacedTo
		}
		rows, err := tx.QueryContext(ctx, orderSearchSQL,
			scope.OrganizationID().String(), scope.WorkspaceID().String(), query.Text, query.Status,
			cursor.ID, cursor.Priority, cursorAt, placedFrom, placedTo, query.Limit+1, likePrefix(query.Text),
		)
		if err != nil {
			return fmt.Errorf("search repository: order query failed: %w", err)
		}
		defer rows.Close()

		type rankedHit struct {
			hit      search.OrderHit
			priority int
		}
		hits := make([]rankedHit, 0, query.Limit+1)
		for rows.Next() {
			var item rankedHit
			if err := rows.Scan(&item.hit.ID, &item.hit.OrderNumber, &item.hit.Status, &item.hit.Currency, &item.hit.GrandMinorUnits, &item.hit.PlacedAt, &item.hit.UpdatedAt, &item.hit.ProductTitle, &item.hit.ProductSKU, &item.hit.ProductImageURL, &item.priority); err != nil {
				return fmt.Errorf("search repository: order row failed: %w", err)
			}
			item.hit.PlacedAt = item.hit.PlacedAt.UTC()
			item.hit.UpdatedAt = item.hit.UpdatedAt.UTC()
			if item.priority < 0 || item.priority > 2 || item.hit.Validate() != nil {
				return search.ErrInvalid
			}
			hits = append(hits, item)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("search repository: order rows failed: %w", err)
		}
		if len(hits) > query.Limit {
			last := hits[query.Limit-1]
			next, err := search.NewCursor(last.priority, last.hit.UpdatedAt, last.hit.ID, fingerprint)
			if err != nil {
				return err
			}
			page.NextCursor = next
			hits = hits[:query.Limit]
		}
		page.Items = make([]search.OrderHit, len(hits))
		for i := range hits {
			page.Items[i] = hits[i].hit
		}
		return page.Validate()
	})
	return page, err
}

func likePrefix(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value) + "%"
}

func (r *Repository) withReadTx(ctx context.Context, scope tenancy.Scope, fn func(*sql.Tx) error) error {
	if ctx == nil || r == nil || r.db == nil || !scope.Valid() || fn == nil {
		return search.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: true})
	if err != nil {
		return fmt.Errorf("search repository: begin read transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var org, workspace string
	if err := tx.QueryRowContext(ctx, applyScope, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&org, &workspace); err != nil {
		return fmt.Errorf("search repository: apply tenant scope: %w", err)
	}
	if org != scope.OrganizationID().String() || workspace != scope.WorkspaceID().String() {
		return search.ErrInvalid
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("search repository: commit read transaction: %w", err)
	}
	return nil
}

// Package marketplacelisting defines the provider-neutral listing workspace.
// It keeps channel taxonomy, mapping and batch evidence separate from the
// canonical Product/Offer/PIM records.
package marketplacelisting

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalid        = errors.New("marketplace listing: invalid value")
	ErrConflict       = errors.New("marketplace listing: conflict")
	ErrStaleTaxonomy  = errors.New("marketplace listing: taxonomy is stale")
	ErrBatchTooLarge  = errors.New("marketplace listing: batch exceeds 1000 SKU limit")
	ErrApprovalNeeded = errors.New("marketplace listing: approval is required")
)

const MaxBatchItems = 1000

var (
	refPattern         = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$`)
	codePattern        = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,127}$`)
	localePattern      = regexp.MustCompile(`^[a-z]{2}(?:-[A-Z]{2})?$`)
	countryPattern     = regexp.MustCompile(`^[A-Z]{2}$`)
	decimalPattern     = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]{1,9})?$`)
	integerPattern     = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)$`)
	datePattern        = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
	digestPattern      = regexp.MustCompile(`^[0-9a-f]{64}$`)
	mediaFormatPattern = regexp.MustCompile(`^[a-z0-9]+/[a-z0-9.+-]+$`)
)

type Requirement string

const (
	RequirementOptional    Requirement = "optional"
	RequirementRequired    Requirement = "required"
	RequirementConditional Requirement = "conditional"
)

func (r Requirement) Valid() bool {
	return r == RequirementOptional || r == RequirementRequired || r == RequirementConditional
}

type ValueType string

const (
	ValueText      ValueType = "text"
	ValueEnum      ValueType = "enum"
	ValueMultiEnum ValueType = "multi_enum"
	ValueInteger   ValueType = "integer"
	ValueDecimal   ValueType = "decimal"
	ValueBoolean   ValueType = "boolean"
	ValueDimension ValueType = "dimension"
	ValueWeight    ValueType = "weight"
	ValueDate      ValueType = "date"
	ValueMedia     ValueType = "media"
)

func (v ValueType) Valid() bool {
	switch v {
	case ValueText, ValueEnum, ValueMultiEnum, ValueInteger, ValueDecimal, ValueBoolean, ValueDimension, ValueWeight, ValueDate, ValueMedia:
		return true
	default:
		return false
	}
}

type EnumValue struct {
	Code       string `json:"code"`
	Label      string `json:"label"`
	Deprecated bool   `json:"deprecated,omitempty"`
}

type Condition struct {
	WhenField string `json:"when_field"`
	Equals    string `json:"equals"`
}

func (c Condition) Validate() error {
	if !codePattern.MatchString(c.WhenField) || c.Equals == "" || len(c.Equals) > 256 {
		return ErrInvalid
	}
	return nil
}

type AttributeDefinition struct {
	Code          string            `json:"code"`
	Name          string            `json:"name"`
	ValueType     ValueType         `json:"value_type"`
	Requirement   Requirement       `json:"requirement"`
	Unit          string            `json:"unit,omitempty"`
	EnumValues    []EnumValue       `json:"enum_values,omitempty"`
	Conditions    []Condition       `json:"conditions,omitempty"`
	Min           string            `json:"min,omitempty"`
	Max           string            `json:"max,omitempty"`
	LocalizedName map[string]string `json:"localized_name,omitempty"`
}

func (a AttributeDefinition) Validate() error {
	if !codePattern.MatchString(a.Code) || strings.TrimSpace(a.Name) != a.Name || a.Name == "" || len(a.Name) > 300 || !a.ValueType.Valid() || !a.Requirement.Valid() || (a.Unit != "" && !codePattern.MatchString(a.Unit)) || len(a.EnumValues) > 256 || len(a.Conditions) > 16 {
		return ErrInvalid
	}
	if a.Requirement == RequirementConditional && len(a.Conditions) == 0 {
		return ErrInvalid
	}
	seen := map[string]struct{}{}
	for _, item := range a.EnumValues {
		if !codePattern.MatchString(item.Code) || strings.TrimSpace(item.Label) != item.Label || item.Label == "" || len(item.Label) > 300 {
			return ErrInvalid
		}
		if _, ok := seen[item.Code]; ok {
			return ErrConflict
		}
		seen[item.Code] = struct{}{}
	}
	for _, condition := range a.Conditions {
		if condition.Validate() != nil {
			return ErrInvalid
		}
	}
	if a.Min != "" && !decimalPattern.MatchString(a.Min) || a.Max != "" && !decimalPattern.MatchString(a.Max) {
		return ErrInvalid
	}
	for locale, label := range a.LocalizedName {
		if !localePattern.MatchString(locale) || strings.TrimSpace(label) != label || label == "" || len(label) > 300 {
			return ErrInvalid
		}
	}
	return nil
}

type Category struct {
	Code           string   `json:"code"`
	Name           string   `json:"name"`
	ParentCode     string   `json:"parent_code,omitempty"`
	AttributeCodes []string `json:"attribute_codes,omitempty"`
}

func (c Category) Validate() error {
	if !codePattern.MatchString(c.Code) || strings.TrimSpace(c.Name) != c.Name || c.Name == "" || len(c.Name) > 300 || (c.ParentCode != "" && !codePattern.MatchString(c.ParentCode)) || len(c.AttributeCodes) > 512 {
		return ErrInvalid
	}
	seen := map[string]struct{}{}
	for _, code := range c.AttributeCodes {
		if !codePattern.MatchString(code) {
			return ErrInvalid
		}
		if _, ok := seen[code]; ok {
			return ErrConflict
		}
		seen[code] = struct{}{}
	}
	return nil
}

type MediaSlot struct {
	Code      string   `json:"code"`
	Name      string   `json:"name"`
	Required  bool     `json:"required"`
	MaxItems  int      `json:"max_items"`
	Formats   []string `json:"formats,omitempty"`
	MinWidth  int      `json:"min_width,omitempty"`
	MinHeight int      `json:"min_height,omitempty"`
}

func (m MediaSlot) Validate() error {
	if !codePattern.MatchString(m.Code) || strings.TrimSpace(m.Name) != m.Name || m.Name == "" || len(m.Name) > 300 || m.MaxItems < 1 || m.MaxItems > 64 || m.MinWidth < 0 || m.MinHeight < 0 || len(m.Formats) > 32 {
		return ErrInvalid
	}
	for _, format := range m.Formats {
		if !mediaFormatPattern.MatchString(strings.ToLower(format)) {
			return ErrInvalid
		}
	}
	return nil
}

type Taxonomy struct {
	ID           string                `json:"id"`
	ChannelID    string                `json:"connector_id"`
	Locale       string                `json:"locale"`
	Jurisdiction string                `json:"jurisdiction"`
	Version      int64                 `json:"version"`
	Source       string                `json:"source"`
	Fingerprint  string                `json:"fingerprint"`
	ObservedAt   time.Time             `json:"observed_at"`
	FreshUntil   time.Time             `json:"fresh_until"`
	Categories   []Category            `json:"categories"`
	Attributes   []AttributeDefinition `json:"attributes"`
	MediaSlots   []MediaSlot           `json:"media_slots"`
}

func (t Taxonomy) Validate() error {
	if !refPattern.MatchString(t.ID) || !refPattern.MatchString(t.ChannelID) || !localePattern.MatchString(t.Locale) || !countryPattern.MatchString(t.Jurisdiction) || t.Version < 1 || strings.TrimSpace(t.Source) != t.Source || t.Source == "" || len(t.Source) > 256 || !isUTC(t.ObservedAt) || !isUTC(t.FreshUntil) || !t.FreshUntil.After(t.ObservedAt) || len(t.Categories) > 50_000 || len(t.Attributes) > 512 || len(t.MediaSlots) > 64 {
		return ErrInvalid
	}
	if t.Fingerprint != "" && !isDigest(t.Fingerprint) {
		return ErrInvalid
	}
	seen := map[string]struct{}{}
	for _, category := range t.Categories {
		if category.Validate() != nil {
			return ErrInvalid
		}
		if _, ok := seen[category.Code]; ok {
			return ErrConflict
		}
		seen[category.Code] = struct{}{}
	}
	seen = map[string]struct{}{}
	for _, attribute := range t.Attributes {
		if attribute.Validate() != nil {
			return ErrInvalid
		}
		if _, ok := seen[attribute.Code]; ok {
			return ErrConflict
		}
		seen[attribute.Code] = struct{}{}
	}
	attributes := make(map[string]struct{}, len(t.Attributes))
	for _, attribute := range t.Attributes {
		attributes[attribute.Code] = struct{}{}
	}
	for _, attribute := range t.Attributes {
		for _, condition := range attribute.Conditions {
			if _, ok := attributes[condition.WhenField]; !ok {
				return ErrInvalid
			}
		}
	}
	categoryCodes := make(map[string]struct{}, len(t.Categories))
	for _, category := range t.Categories {
		categoryCodes[category.Code] = struct{}{}
	}
	for _, category := range t.Categories {
		if category.ParentCode != "" {
			if category.ParentCode == category.Code {
				return ErrConflict
			}
			if _, ok := categoryCodes[category.ParentCode]; !ok {
				return ErrInvalid
			}
		}
		for _, attributeCode := range category.AttributeCodes {
			if _, ok := attributes[attributeCode]; !ok {
				return ErrInvalid
			}
		}
	}
	for _, category := range t.Categories {
		seenParents := map[string]struct{}{category.Code: {}}
		parent := category.ParentCode
		for parent != "" {
			if _, seen := seenParents[parent]; seen {
				return ErrConflict
			}
			seenParents[parent] = struct{}{}
			current, ok := findCategory(t, parent)
			if !ok {
				return ErrInvalid
			}
			parent = current.ParentCode
		}
	}
	seen = map[string]struct{}{}
	for _, slot := range t.MediaSlots {
		if slot.Validate() != nil {
			return ErrInvalid
		}
		if _, ok := seen[slot.Code]; ok {
			return ErrConflict
		}
		seen[slot.Code] = struct{}{}
	}
	return nil
}

func (t Taxonomy) ComputeFingerprint() (string, error) {
	copyTaxonomy := t
	copyTaxonomy.Fingerprint = ""
	data, err := json.Marshal(copyTaxonomy)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func (t Taxonomy) Fresh(now time.Time) bool {
	return t.Validate() == nil && isUTC(now) && now.Before(t.FreshUntil)
}

type AttributeValue struct {
	Value string `json:"value"`
	Unit  string `json:"unit,omitempty"`
}

type Content struct {
	Locale       string   `json:"locale"`
	Title        string   `json:"title"`
	Description  string   `json:"description,omitempty"`
	Bullets      []string `json:"bullets,omitempty"`
	Brand        string   `json:"brand,omitempty"`
	Provenance   string   `json:"provenance,omitempty"`
	ModelID      string   `json:"model_id,omitempty"`
	PromptDigest string   `json:"prompt_digest,omitempty"`
}

func (c Content) Validate() error {
	if !localePattern.MatchString(c.Locale) || !validText(c.Title, 500) || !validOptionalText(c.Description, 10_000) || len(c.Bullets) > 32 || len(c.Brand) > 300 || (c.Provenance != "" && !codePattern.MatchString(c.Provenance)) || len(c.ModelID) > 192 || (c.PromptDigest != "" && !isDigest(c.PromptDigest)) {
		return ErrInvalid
	}
	for _, bullet := range c.Bullets {
		if !validText(bullet, 500) {
			return ErrInvalid
		}
	}
	return nil
}

type MediaRef struct {
	ID                string `json:"id"`
	Slot              string `json:"slot"`
	ReleasedObjectRef string `json:"released_object_ref"`
	Digest            string `json:"digest"`
	Format            string `json:"format"`
	Bytes             int64  `json:"bytes"`
	Width             int    `json:"width"`
	Height            int    `json:"height"`
	Position          int    `json:"position"`
	Released          bool   `json:"released"`
	Safe              bool   `json:"safe"`
}

func (m MediaRef) Validate() error {
	if !refPattern.MatchString(m.ID) || !codePattern.MatchString(m.Slot) || !strings.HasPrefix(m.ReleasedObjectRef, "upl_") || !refPattern.MatchString(m.ReleasedObjectRef) || !isDigest(m.Digest) || !mediaFormatPattern.MatchString(strings.ToLower(m.Format)) || m.Bytes < 1 || m.Bytes > 100*1024*1024 || m.Width < 0 || m.Height < 0 || m.Position < 0 || m.Position > 1000 {
		return ErrInvalid
	}
	return nil
}

type Variant struct {
	ID         string                    `json:"id"`
	SKU        string                    `json:"sku"`
	Axes       map[string]string         `json:"axes,omitempty"`
	Attributes map[string]AttributeValue `json:"attributes,omitempty"`
}

func (v Variant) Validate() error {
	if !refPattern.MatchString(v.ID) || !validText(v.SKU, 200) || len(v.Axes) > 16 || len(v.Attributes) > 128 {
		return ErrInvalid
	}
	for key, value := range v.Axes {
		if !codePattern.MatchString(key) || !validText(value, 256) {
			return ErrInvalid
		}
	}
	for key, value := range v.Attributes {
		if !codePattern.MatchString(key) || !validText(value.Value, 2000) || (value.Unit != "" && !codePattern.MatchString(value.Unit)) {
			return ErrInvalid
		}
	}
	return nil
}

type ListingDraft struct {
	ID                  string                    `json:"id"`
	OrganizationID      string                    `json:"organization_id"`
	WorkspaceID         string                    `json:"workspace_id"`
	ProductID           string                    `json:"product_id"`
	OfferID             string                    `json:"offer_id,omitempty"`
	SKU                 string                    `json:"sku"`
	CategoryCode        string                    `json:"category_code"`
	TaxonomyFingerprint string                    `json:"taxonomy_fingerprint"`
	ProductVersion      int64                     `json:"product_version"`
	OfferVersion        int64                     `json:"offer_version"`
	Attributes          map[string]AttributeValue `json:"attributes,omitempty"`
	Content             Content                   `json:"content"`
	Variants            []Variant                 `json:"variants,omitempty"`
	Media               []MediaRef                `json:"media,omitempty"`
}

func (d ListingDraft) Validate() error {
	if !refPattern.MatchString(d.ID) || !refPattern.MatchString(d.OrganizationID) || !refPattern.MatchString(d.WorkspaceID) || !refPattern.MatchString(d.ProductID) || (d.OfferID != "" && !refPattern.MatchString(d.OfferID)) || !validText(d.SKU, 200) || !codePattern.MatchString(d.CategoryCode) || !isDigest(d.TaxonomyFingerprint) || d.ProductVersion < 1 || d.OfferVersion < 0 || len(d.Attributes) > 512 || len(d.Variants) > 100 || len(d.Media) > 64 || d.Content.Validate() != nil {
		return ErrInvalid
	}
	for key, value := range d.Attributes {
		if !codePattern.MatchString(key) || !validText(value.Value, 2000) || (value.Unit != "" && !codePattern.MatchString(value.Unit)) {
			return ErrInvalid
		}
	}
	seen := map[string]struct{}{}
	variantCombinations := map[string]struct{}{}
	for _, variant := range d.Variants {
		if variant.Validate() != nil {
			return ErrInvalid
		}
		if variant.SKU == d.SKU {
			return ErrConflict
		}
		if _, ok := seen[variant.SKU]; ok {
			return ErrConflict
		}
		seen[variant.SKU] = struct{}{}
		combination := mapSignature(variant.Axes)
		if _, ok := variantCombinations[combination]; ok {
			return ErrConflict
		}
		variantCombinations[combination] = struct{}{}
	}
	seen = map[string]struct{}{}
	for _, media := range d.Media {
		if media.Validate() != nil {
			return ErrInvalid
		}
		if _, ok := seen[media.ID]; ok {
			return ErrConflict
		}
		seen[media.ID] = struct{}{}
	}
	return nil
}

type Severity string

const (
	SeverityBlock Severity = "block"
	SeverityWarn  Severity = "warn"
)

type Diagnostic struct {
	Code        string   `json:"code"`
	Severity    Severity `json:"severity"`
	FieldPath   string   `json:"field_path"`
	Message     string   `json:"message"`
	Expected    string   `json:"expected,omitempty"`
	Observed    string   `json:"observed,omitempty"`
	Remediation string   `json:"remediation"`
}

func (d Diagnostic) Validate() error {
	if !codePattern.MatchString(d.Code) || (d.Severity != SeverityBlock && d.Severity != SeverityWarn) || len(d.FieldPath) > 256 || d.Message == "" || len(d.Message) > 300 || len(d.Expected) > 256 || len(d.Observed) > 256 || d.Remediation == "" || len(d.Remediation) > 300 {
		return ErrInvalid
	}
	return nil
}

// ValidateDraft checks taxonomy, content, media, variants and conditional
// attributes without mutating the canonical PIM or Product records.
func ValidateDraft(taxonomy Taxonomy, draft ListingDraft, now time.Time) []Diagnostic {
	issues := make([]Diagnostic, 0, 8)
	add := func(code, field, message, expected, observed, remediation string, severity Severity) {
		issues = append(issues, Diagnostic{Code: code, FieldPath: field, Message: message, Expected: expected, Observed: observed, Remediation: remediation, Severity: severity})
	}
	if taxonomy.Validate() != nil || draft.Validate() != nil {
		add("invalid_input", "listing", "Карточка содержит недопустимые значения", "валидная карточка", "invalid", "Исправьте данные карточки", SeverityBlock)
		return issues
	}
	fingerprint, _ := taxonomy.ComputeFingerprint()
	if !taxonomy.Fresh(now) {
		add("stale_taxonomy", "taxonomy", "Схема канала устарела", "актуальная taxonomy", strconv.FormatInt(taxonomy.Version, 10), "Обновите схему канала", SeverityBlock)
	}
	if draft.TaxonomyFingerprint != fingerprint {
		add("taxonomy_mismatch", "taxonomy_fingerprint", "Карточка собрана по другой версии taxonomy", fingerprint, draft.TaxonomyFingerprint, "Пересоберите mapping по текущей схеме", SeverityBlock)
	}
	category, categoryOK := findCategory(taxonomy, draft.CategoryCode)
	if !categoryOK {
		add("unknown_category", "category_code", "Категория отсутствует в taxonomy канала", "существующая категория", draft.CategoryCode, "Выберите категорию из актуального дерева", SeverityBlock)
	}
	definitions := make(map[string]AttributeDefinition, len(taxonomy.Attributes))
	for _, definition := range taxonomy.Attributes {
		definitions[definition.Code] = definition
	}
	allowed := map[string]bool{}
	if categoryOK && len(category.AttributeCodes) > 0 {
		for _, code := range category.AttributeCodes {
			allowed[code] = true
		}
	}
	for code := range draft.Attributes {
		if _, ok := definitions[code]; !ok || len(allowed) > 0 && !allowed[code] {
			add("unsupported_attribute", "attributes."+code, "Атрибут не поддерживается выбранной категорией", "атрибут из схемы категории", code, "Удалите атрибут или выберите другую категорию", SeverityBlock)
		}
	}
	for _, definition := range taxonomy.Attributes {
		if len(allowed) > 0 && !allowed[definition.Code] {
			continue
		}
		value, present := draft.Attributes[definition.Code]
		required := definition.Requirement == RequirementRequired || definition.Requirement == RequirementConditional && conditionMatches(definition.Conditions, draft.Attributes)
		if required && (!present || strings.TrimSpace(value.Value) == "") {
			add("missing_attribute", "attributes."+definition.Code, "Не заполнен обязательный атрибут", definition.Name, "отсутствует", "Заполните поле карточки", SeverityBlock)
			continue
		}
		if present && strings.TrimSpace(value.Value) != "" {
			validateAttributeValue(definition, value, add)
		}
	}
	if len(draft.Media) == 0 {
		for _, slot := range taxonomy.MediaSlots {
			if slot.Required {
				add("missing_media", "media."+slot.Code, "Не загружено обязательное изображение", slot.Name, "отсутствует", "Добавьте released и проверенный asset", SeverityBlock)
			}
		}
	}
	for _, slot := range taxonomy.MediaSlots {
		count := 0
		for _, media := range draft.Media {
			if media.Slot == slot.Code {
				count++
				if !media.Released || !media.Safe {
					add("media_not_released", "media."+slot.Code, "Изображение ещё не прошло security release", "released=true и safe=true", media.ID, "Дождитесь проверки загрузки", SeverityBlock)
				}
				if len(slot.Formats) > 0 && !containsFold(slot.Formats, media.Format) {
					add("invalid_media_format", "media."+slot.Code, "Формат изображения не разрешён каналом", strings.Join(slot.Formats, ", "), media.Format, "Замените asset на разрешённый формат", SeverityBlock)
				}
				if media.Width < slot.MinWidth || media.Height < slot.MinHeight {
					add("invalid_media_dimensions", "media."+slot.Code, "Размер изображения меньше ограничения канала", strconv.Itoa(slot.MinWidth)+"x"+strconv.Itoa(slot.MinHeight), strconv.Itoa(media.Width)+"x"+strconv.Itoa(media.Height), "Загрузите изображение нужного размера", SeverityBlock)
				}
			}
		}
		if count > slot.MaxItems {
			add("media_slot_overflow", "media."+slot.Code, "В media slot слишком много файлов", strconv.Itoa(slot.MaxItems), strconv.Itoa(count), "Удалите лишние файлы после preview", SeverityBlock)
		}
	}
	if draft.Content.Provenance != "" && draft.Content.Provenance == "ai" && draft.Content.ModelID == "" {
		add("missing_ai_provenance", "content.provenance", "AI-контент не содержит provenance", "model_id и prompt_digest", "отсутствует", "Сохраните provenance и оставьте результат draft", SeverityBlock)
	}
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].FieldPath != issues[j].FieldPath {
			return issues[i].FieldPath < issues[j].FieldPath
		}
		return issues[i].Code < issues[j].Code
	})
	return issues
}

func validateAttributeValue(definition AttributeDefinition, value AttributeValue, add func(string, string, string, string, string, string, Severity)) {
	field := "attributes." + definition.Code
	if definition.Unit != "" && value.Unit != definition.Unit {
		add("invalid_unit", field, "Единица измерения не совпадает со схемой", definition.Unit, value.Unit, "Выберите единицу из списка", SeverityBlock)
	}
	valid := true
	switch definition.ValueType {
	case ValueInteger:
		valid = integerPattern.MatchString(value.Value)
	case ValueDecimal, ValueDimension, ValueWeight:
		valid = decimalPattern.MatchString(value.Value)
	case ValueBoolean:
		valid = value.Value == "true" || value.Value == "false"
	case ValueDate:
		valid = datePattern.MatchString(value.Value)
	case ValueEnum:
		valid = enumAllowed(definition.EnumValues, value.Value)
	case ValueMultiEnum:
		parts := strings.Split(value.Value, ",")
		valid = len(parts) > 0
		for _, part := range parts {
			if !enumAllowed(definition.EnumValues, strings.TrimSpace(part)) {
				valid = false
			}
		}
	case ValueMedia:
		valid = strings.HasPrefix(value.Value, "media:") && len(value.Value) > len("media:")
	case ValueText:
		valid = validText(value.Value, 2000)
	}
	if !valid {
		add("invalid_attribute_value", field, "Значение не соответствует типу или enum канала", string(definition.ValueType), value.Value, "Исправьте значение по подсказке схемы", SeverityBlock)
	}
	if definition.Min != "" && decimalPattern.MatchString(value.Value) && compareDecimal(value.Value, definition.Min) < 0 {
		add("attribute_below_min", field, "Значение меньше минимального", definition.Min, value.Value, "Укажите значение в допустимом диапазоне", SeverityBlock)
	}
	if definition.Max != "" && decimalPattern.MatchString(value.Value) && compareDecimal(value.Value, definition.Max) > 0 {
		add("attribute_above_max", field, "Значение больше максимального", definition.Max, value.Value, "Укажите значение в допустимом диапазоне", SeverityBlock)
	}
}

func conditionMatches(conditions []Condition, values map[string]AttributeValue) bool {
	if len(conditions) == 0 {
		return false
	}
	for _, condition := range conditions {
		value, ok := values[condition.WhenField]
		if !ok || value.Value != condition.Equals {
			return false
		}
	}
	return true
}

func enumAllowed(values []EnumValue, value string) bool {
	for _, item := range values {
		if item.Code == value && !item.Deprecated {
			return true
		}
	}
	return false
}

type MappingEntry struct {
	SourceField string            `json:"source_field"`
	TargetCode  string            `json:"target_code"`
	Transform   string            `json:"transform,omitempty"`
	EnumMap     map[string]string `json:"enum_map,omitempty"`
	UnitFrom    string            `json:"unit_from,omitempty"`
	UnitTo      string            `json:"unit_to,omitempty"`
}

type Mapping struct {
	ID                  string         `json:"id"`
	Version             int64          `json:"version"`
	TaxonomyFingerprint string         `json:"taxonomy_fingerprint"`
	Entries             []MappingEntry `json:"entries"`
	Manual              bool           `json:"manual"`
	Reason              string         `json:"reason,omitempty"`
}

func (m Mapping) Validate() error {
	if !refPattern.MatchString(m.ID) || m.Version < 1 || !isDigest(m.TaxonomyFingerprint) || len(m.Entries) > 512 || (m.Manual && strings.TrimSpace(m.Reason) == "") {
		return ErrInvalid
	}
	seen := map[string]struct{}{}
	for _, entry := range m.Entries {
		if !codePattern.MatchString(entry.SourceField) || !codePattern.MatchString(entry.TargetCode) || !validMappingTransform(entry.Transform) || len(entry.EnumMap) > 256 || (entry.UnitFrom != "" && !codePattern.MatchString(entry.UnitFrom)) || (entry.UnitTo != "" && !codePattern.MatchString(entry.UnitTo)) || (entry.UnitFrom == "") != (entry.UnitTo == "") {
			return ErrInvalid
		}
		if _, ok := seen[entry.TargetCode]; ok {
			return ErrConflict
		}
		seen[entry.TargetCode] = struct{}{}
		for from, to := range entry.EnumMap {
			if !validText(from, 256) || !validText(to, 256) {
				return ErrInvalid
			}
		}
	}
	return nil
}

// ApplyMapping is deterministic and refuses a mapping built for another
// taxonomy fingerprint. Unit conversion uses integer millimetres/grams only.
func ApplyMapping(mapping Mapping, taxonomy Taxonomy, source map[string]AttributeValue) (map[string]AttributeValue, error) {
	if mapping.Validate() != nil || taxonomy.Validate() != nil {
		return nil, ErrInvalid
	}
	fingerprint, _ := taxonomy.ComputeFingerprint()
	if mapping.TaxonomyFingerprint != fingerprint {
		return nil, ErrStaleTaxonomy
	}
	result := make(map[string]AttributeValue, len(mapping.Entries))
	definitions := make(map[string]AttributeDefinition, len(taxonomy.Attributes))
	for _, definition := range taxonomy.Attributes {
		definitions[definition.Code] = definition
	}
	for _, entry := range mapping.Entries {
		definition, exists := definitions[entry.TargetCode]
		if !exists {
			return nil, ErrInvalid
		}
		value, ok := source[entry.SourceField]
		if !ok {
			continue
		}
		if mapped, exists := entry.EnumMap[value.Value]; exists {
			value.Value = mapped
		}
		switch entry.Transform {
		case "trim":
			value.Value = strings.TrimSpace(value.Value)
		case "lower":
			value.Value = strings.ToLower(value.Value)
		case "upper":
			value.Value = strings.ToUpper(value.Value)
		case "normalize_decimal":
			if !decimalPattern.MatchString(value.Value) {
				return nil, ErrInvalid
			}
		}
		if entry.UnitTo != "" {
			if value.Unit != entry.UnitFrom {
				return nil, ErrInvalid
			}
			converted, err := convertUnit(value.Value, entry.UnitFrom, entry.UnitTo)
			if err != nil {
				return nil, ErrInvalid
			}
			value.Value = converted
			value.Unit = entry.UnitTo
		}
		if definition.Unit != "" && value.Unit != definition.Unit {
			return nil, ErrInvalid
		}
		result[entry.TargetCode] = value
	}
	return result, nil
}

type BatchOperationKind string

const (
	BatchSet       BatchOperationKind = "set"
	BatchReplace   BatchOperationKind = "replace"
	BatchMap       BatchOperationKind = "map"
	BatchAppend    BatchOperationKind = "append"
	BatchRemove    BatchOperationKind = "remove"
	BatchNormalize BatchOperationKind = "normalize"
	BatchCopy      BatchOperationKind = "copy"
)

func (k BatchOperationKind) Valid() bool {
	return k == BatchSet || k == BatchReplace || k == BatchMap || k == BatchAppend || k == BatchRemove || k == BatchNormalize || k == BatchCopy
}

type BatchOperation struct {
	Kind      BatchOperationKind `json:"kind"`
	Field     string             `json:"field"`
	Value     string             `json:"value,omitempty"`
	FromField string             `json:"from_field,omitempty"`
}

func (o BatchOperation) Validate() error {
	if !o.Kind.Valid() || !validBatchField(o.Field) || len(o.Value) > 2000 || (o.FromField != "" && !validBatchField(o.FromField)) {
		return ErrInvalid
	}
	if o.Kind == BatchCopy && o.FromField == "" {
		return ErrInvalid
	}
	if (o.Kind == BatchSet || o.Kind == BatchReplace || o.Kind == BatchMap || o.Kind == BatchAppend || o.Kind == BatchCopy) && o.Value == "" && o.FromField == "" {
		return ErrInvalid
	}
	return nil
}

type BatchItem struct {
	SKU    string       `json:"sku"`
	Before ListingDraft `json:"before"`
}

type BatchRow struct {
	SKU          string       `json:"sku"`
	BeforeDigest string       `json:"before_digest"`
	AfterDigest  string       `json:"after_digest"`
	Before       ListingDraft `json:"before"`
	After        ListingDraft `json:"after"`
	Changed      bool         `json:"changed"`
	Eligible     bool         `json:"eligible"`
	Diagnostics  []Diagnostic `json:"diagnostics,omitempty"`
}

type BatchPreview struct {
	ID                  string     `json:"id"`
	OrganizationID      string     `json:"organization_id"`
	WorkspaceID         string     `json:"workspace_id"`
	ChannelAccountID    string     `json:"connector_account_id"`
	ChannelID           string     `json:"connector_id"`
	TaxonomyFingerprint string     `json:"taxonomy_fingerprint"`
	RuleVersion         int64      `json:"rule_version"`
	InputDigest         string     `json:"input_digest"`
	AffectedCount       int        `json:"affected_count"`
	EligibleCount       int        `json:"eligible_count"`
	BlockedCount        int        `json:"blocked_count"`
	Rows                []BatchRow `json:"rows"`
	CreatedAt           time.Time  `json:"created_at"`
}

func (p BatchPreview) Validate() error {
	if !refPattern.MatchString(p.ID) || !refPattern.MatchString(p.OrganizationID) || !refPattern.MatchString(p.WorkspaceID) || !refPattern.MatchString(p.ChannelAccountID) || !refPattern.MatchString(p.ChannelID) || !isDigest(p.TaxonomyFingerprint) || p.RuleVersion < 1 || !isDigest(p.InputDigest) || p.AffectedCount < 0 || p.AffectedCount > MaxBatchItems || p.EligibleCount < 0 || p.BlockedCount < 0 || p.EligibleCount+p.BlockedCount != p.AffectedCount || len(p.Rows) != p.AffectedCount || !isUTC(p.CreatedAt) {
		return ErrInvalid
	}
	for _, row := range p.Rows {
		if !validText(row.SKU, 200) || !isDigest(row.BeforeDigest) || !isDigest(row.AfterDigest) || row.Before.Validate() != nil || row.After.Validate() != nil {
			return ErrInvalid
		}
		for _, diagnostic := range row.Diagnostics {
			if diagnostic.Validate() != nil {
				return ErrInvalid
			}
		}
	}
	return nil
}

// BuildBatchPreview produces a bounded, immutable before/after diff. Rows are
// sorted by SKU, making the digest independent from selection order.
func BuildBatchPreview(id, organizationID, workspaceID, accountID, channelID string, taxonomy Taxonomy, items []BatchItem, operations []BatchOperation, now time.Time) (BatchPreview, error) {
	if len(items) == 0 {
		return BatchPreview{}, ErrInvalid
	}
	if len(items) > MaxBatchItems {
		return BatchPreview{}, ErrBatchTooLarge
	}
	if taxonomy.Validate() != nil || !isUTC(now) {
		return BatchPreview{}, ErrInvalid
	}
	for _, operation := range operations {
		if operation.Validate() != nil {
			return BatchPreview{}, ErrInvalid
		}
	}
	fingerprint, _ := taxonomy.ComputeFingerprint()
	rowsInput := append([]BatchItem(nil), items...)
	sort.Slice(rowsInput, func(i, j int) bool { return rowsInput[i].SKU < rowsInput[j].SKU })
	seen := map[string]struct{}{}
	rows := make([]BatchRow, 0, len(rowsInput))
	for _, item := range rowsInput {
		if _, ok := seen[item.SKU]; ok || item.Before.SKU != item.SKU {
			return BatchPreview{}, ErrConflict
		}
		seen[item.SKU] = struct{}{}
		beforeDigest, err := DraftDigest(item.Before)
		if err != nil {
			return BatchPreview{}, ErrInvalid
		}
		after := applyBatchOperations(item.Before, operations)
		afterDigest, err := DraftDigest(after)
		if err != nil {
			return BatchPreview{}, ErrInvalid
		}
		diagnostics := ValidateDraft(taxonomy, after, now)
		eligible := true
		for _, diagnostic := range diagnostics {
			if diagnostic.Severity == SeverityBlock {
				eligible = false
				break
			}
		}
		rows = append(rows, BatchRow{SKU: item.SKU, BeforeDigest: beforeDigest, AfterDigest: afterDigest, Before: item.Before, After: after, Changed: beforeDigest != afterDigest, Eligible: eligible, Diagnostics: diagnostics})
	}
	input := struct {
		TaxonomyFingerprint string           `json:"taxonomy_fingerprint"`
		Items               []BatchItem      `json:"items"`
		Operations          []BatchOperation `json:"operations"`
	}{fingerprint, rowsInput, operations}
	inputJSON, _ := json.Marshal(input)
	inputSum := sha256.Sum256(inputJSON)
	preview := BatchPreview{ID: id, OrganizationID: organizationID, WorkspaceID: workspaceID, ChannelAccountID: accountID, ChannelID: channelID, TaxonomyFingerprint: fingerprint, RuleVersion: 1, InputDigest: hex.EncodeToString(inputSum[:]), AffectedCount: len(rows), Rows: rows, CreatedAt: now}
	for _, row := range rows {
		if row.Eligible {
			preview.EligibleCount++
		} else {
			preview.BlockedCount++
		}
	}
	if preview.Validate() != nil {
		return BatchPreview{}, ErrInvalid
	}
	return preview, nil
}

func applyBatchOperations(draft ListingDraft, operations []BatchOperation) ListingDraft {
	result := draft
	result.Attributes = cloneAttributes(draft.Attributes)
	result.Content.Bullets = append([]string(nil), draft.Content.Bullets...)
	for _, operation := range operations {
		switch {
		case operation.Kind == BatchCopy && operation.Field == "content.title":
			result.Content.Title = batchSourceText(result, operation.FromField)
		case operation.Kind == BatchCopy && operation.Field == "content.description":
			result.Content.Description = batchSourceText(result, operation.FromField)
		case operation.Field == "category_code":
			if operation.Kind == BatchSet || operation.Kind == BatchReplace || operation.Kind == BatchMap {
				result.CategoryCode = operation.Value
			}
		case operation.Field == "content.title":
			result.Content.Title = applyText(result.Content.Title, operation)
		case operation.Field == "content.description":
			result.Content.Description = applyText(result.Content.Description, operation)
		case strings.HasPrefix(operation.Field, "attributes."):
			code := strings.TrimPrefix(operation.Field, "attributes.")
			current := result.Attributes[code]
			switch operation.Kind {
			case BatchSet, BatchReplace, BatchMap:
				current.Value = operation.Value
			case BatchAppend:
				if current.Value == "" {
					current.Value = operation.Value
				} else {
					current.Value += "," + operation.Value
				}
			case BatchRemove:
				delete(result.Attributes, code)
			case BatchNormalize:
				current.Value = strings.ToLower(strings.TrimSpace(current.Value))
			case BatchCopy:
				if source, ok := result.Attributes[strings.TrimPrefix(operation.FromField, "attributes.")]; ok {
					current = source
				}
			}
			if operation.Kind != BatchRemove {
				result.Attributes[code] = current
			}
		}
	}
	return result
}

func applyText(current string, operation BatchOperation) string {
	switch operation.Kind {
	case BatchSet, BatchReplace, BatchMap:
		if operation.Kind == BatchReplace && operation.FromField != "" {
			return strings.ReplaceAll(current, operation.FromField, operation.Value)
		}
		return operation.Value
	case BatchAppend:
		if current == "" {
			return operation.Value
		}
		return current + " " + operation.Value
	case BatchRemove:
		return strings.ReplaceAll(current, operation.Value, "")
	case BatchNormalize:
		return strings.TrimSpace(current)
	default:
		return current
	}
}

func validMappingTransform(value string) bool {
	switch value {
	case "", "trim", "lower", "upper", "normalize_decimal":
		return true
	default:
		return false
	}
}

func validBatchField(value string) bool {
	switch value {
	case "category_code", "content.title", "content.description":
		return true
	default:
		return strings.HasPrefix(value, "attributes.") && codePattern.MatchString(strings.TrimPrefix(value, "attributes."))
	}
}

func batchSourceText(draft ListingDraft, field string) string {
	switch field {
	case "category_code":
		return draft.CategoryCode
	case "content.title":
		return draft.Content.Title
	case "content.description":
		return draft.Content.Description
	default:
		if strings.HasPrefix(field, "attributes.") {
			return draft.Attributes[strings.TrimPrefix(field, "attributes.")].Value
		}
		return ""
	}
}

func mapSignature(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var result strings.Builder
	for _, key := range keys {
		result.WriteString(key)
		result.WriteByte('=')
		result.WriteString(values[key])
		result.WriteByte(';')
	}
	return result.String()
}

func convertUnit(value, from, to string) (string, error) {
	if from == to {
		return value, nil
	}
	scales := map[string][2]int64{
		"mg": {1, 1000}, "g": {1, 1}, "kg": {1000, 1},
		"mm": {1, 1}, "cm": {10, 1}, "m": {1000, 1},
	}
	fromScale, fromOK := scales[from]
	toScale, toOK := scales[to]
	if !fromOK || !toOK {
		return "", ErrInvalid
	}
	result, ok := new(big.Rat).SetString(value)
	if !ok {
		return "", ErrInvalid
	}
	result.Mul(result, new(big.Rat).SetFrac64(fromScale[0], fromScale[1]))
	result.Quo(result, new(big.Rat).SetFrac64(toScale[0], toScale[1]))
	rendered := result.FloatString(9)
	parsed, ok := new(big.Rat).SetString(rendered)
	if !ok || parsed.Cmp(result) != 0 {
		return "", ErrInvalid
	}
	rendered = strings.TrimRight(strings.TrimRight(rendered, "0"), ".")
	if rendered == "" || rendered == "-0" {
		rendered = "0"
	}
	return rendered, nil
}

func DraftDigest(draft ListingDraft) (string, error) {
	data, err := json.Marshal(draft)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

type BatchState string

const (
	BatchQueued     BatchState = "queued"
	BatchProcessing BatchState = "processing"
	BatchCompleted  BatchState = "completed"
	BatchPartial    BatchState = "partial"
	BatchUnknown    BatchState = "unknown"
	BatchRejected   BatchState = "rejected"
)

func (s BatchState) Valid() bool {
	return s == BatchQueued || s == BatchProcessing || s == BatchCompleted || s == BatchPartial || s == BatchUnknown || s == BatchRejected
}

type BatchRun struct {
	ID             string     `json:"id"`
	PreviewID      string     `json:"preview_id"`
	OrganizationID string     `json:"organization_id"`
	WorkspaceID    string     `json:"workspace_id"`
	IdempotencyKey string     `json:"idempotency_key"`
	ApprovalRef    string     `json:"approval_ref,omitempty"`
	State          BatchState `json:"state"`
	InputDigest    string     `json:"input_digest"`
	Rows           []BatchRow `json:"rows"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (b BatchRun) Validate() error {
	if !refPattern.MatchString(b.ID) || !refPattern.MatchString(b.PreviewID) || !refPattern.MatchString(b.OrganizationID) || !refPattern.MatchString(b.WorkspaceID) || b.IdempotencyKey == "" || len(b.IdempotencyKey) > 128 || !b.State.Valid() || !isDigest(b.InputDigest) || len(b.Rows) > MaxBatchItems || !isUTC(b.CreatedAt) || !isUTC(b.UpdatedAt) || b.UpdatedAt.Before(b.CreatedAt) {
		return ErrInvalid
	}
	for _, row := range b.Rows {
		if validText(row.SKU, 200) != true || !isDigest(row.BeforeDigest) || !isDigest(row.AfterDigest) || row.Before.Validate() != nil || row.After.Validate() != nil {
			return ErrInvalid
		}
	}
	return nil
}

type RemoteObservation struct {
	RemoteID       string    `json:"remote_id"`
	SnapshotDigest string    `json:"snapshot_digest"`
	Status         string    `json:"status"`
	CategoryCode   string    `json:"category_code,omitempty"`
	ObservedAt     time.Time `json:"observed_at"`
}

type DriftType string

const (
	DriftMissingRemote       DriftType = "missing_remote"
	DriftContentMismatch     DriftType = "content_mismatch"
	DriftAttributeMismatch   DriftType = "attribute_mismatch"
	DriftMediaMismatch       DriftType = "media_mismatch"
	DriftCategoryMismatch    DriftType = "category_mismatch"
	DriftPublicationMismatch DriftType = "publication_status_mismatch"
	DriftUnknownOutcome      DriftType = "unknown_write_outcome"
)

func (d DriftType) Valid() bool {
	switch d {
	case DriftMissingRemote, DriftContentMismatch, DriftAttributeMismatch, DriftMediaMismatch, DriftCategoryMismatch, DriftPublicationMismatch, DriftUnknownOutcome:
		return true
	default:
		return false
	}
}

type Drift struct {
	Type           DriftType `json:"type"`
	ExpectedDigest string    `json:"expected_digest"`
	ObservedDigest string    `json:"observed_digest,omitempty"`
	RemoteID       string    `json:"remote_id,omitempty"`
	ObservedStatus string    `json:"observed_status"`
	DetectedAt     time.Time `json:"detected_at"`
}

func (d Drift) Validate() error {
	if !d.Type.Valid() || !isDigest(d.ExpectedDigest) || (d.ObservedDigest != "" && !isDigest(d.ObservedDigest)) || (d.RemoteID != "" && !refPattern.MatchString(d.RemoteID)) || d.ObservedStatus == "" || !isUTC(d.DetectedAt) {
		return ErrInvalid
	}
	return nil
}

func Reconcile(expected ListingDraft, expectedDigest string, observation RemoteObservation) ([]Drift, error) {
	if expected.Validate() != nil || !isDigest(expectedDigest) || !isUTC(observation.ObservedAt) || observation.RemoteID == "" || observation.Status == "" || observation.SnapshotDigest != "" && !isDigest(observation.SnapshotDigest) {
		return nil, ErrInvalid
	}
	drifts := make([]Drift, 0, 3)
	add := func(kind DriftType) {
		drifts = append(drifts, Drift{Type: kind, ExpectedDigest: expectedDigest, ObservedDigest: observation.SnapshotDigest, RemoteID: observation.RemoteID, ObservedStatus: observation.Status, DetectedAt: observation.ObservedAt})
	}
	if observation.Status == "unknown" {
		add(DriftUnknownOutcome)
	}
	if observation.RemoteID == "missing" {
		add(DriftMissingRemote)
	}
	if observation.SnapshotDigest != "" && observation.SnapshotDigest != expectedDigest {
		add(DriftContentMismatch)
	}
	if observation.CategoryCode != "" && observation.CategoryCode != expected.CategoryCode {
		add(DriftCategoryMismatch)
	}
	if observation.Status != "published" && observation.Status != "processing" && observation.Status != "accepted" && observation.Status != "unknown" {
		add(DriftPublicationMismatch)
	}
	sort.Slice(drifts, func(i, j int) bool { return drifts[i].Type < drifts[j].Type })
	return drifts, nil
}

// DemoTaxonomy is deterministic synthetic data for local Compose/demo flows.
// It never claims that a real marketplace schema has been qualified.
func DemoTaxonomy(channelID, locale, jurisdiction string, now time.Time) Taxonomy {
	return Taxonomy{ID: "demo.taxonomy." + channelID, ChannelID: channelID, Locale: locale, Jurisdiction: jurisdiction, Version: 1, Source: "synthetic.demo", ObservedAt: now.UTC(), FreshUntil: now.UTC().Add(24 * time.Hour), Categories: []Category{{Code: "demo.product", Name: "Демонстрационный товар", AttributeCodes: []string{"color", "material"}}}, Attributes: []AttributeDefinition{{Code: "color", Name: "Цвет", ValueType: ValueEnum, Requirement: RequirementRequired, EnumValues: []EnumValue{{Code: "black", Label: "Чёрный"}, {Code: "white", Label: "Белый"}}}, {Code: "material", Name: "Материал", ValueType: ValueText, Requirement: RequirementConditional, Conditions: []Condition{{WhenField: "color", Equals: "black"}}}}, MediaSlots: []MediaSlot{{Code: "main", Name: "Главное изображение", Required: true, MaxItems: 1, Formats: []string{"image/jpeg", "image/png"}, MinWidth: 500, MinHeight: 500}}}
}

func findCategory(t Taxonomy, code string) (Category, bool) {
	for _, category := range t.Categories {
		if category.Code == code {
			return category, true
		}
	}
	return Category{}, false
}
func cloneAttributes(values map[string]AttributeValue) map[string]AttributeValue {
	result := make(map[string]AttributeValue, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}
func containsFold(values []string, value string) bool {
	for _, item := range values {
		if strings.EqualFold(item, value) {
			return true
		}
	}
	return false
}
func validText(value string, max int) bool {
	return value != "" && value == strings.TrimSpace(value) && len([]rune(value)) <= max && !strings.ContainsAny(value, "\x00\r\n")
}
func validOptionalText(value string, max int) bool { return value == "" || validText(value, max) }
func isDigest(value string) bool                   { return len(value) == 64 && digestPattern.MatchString(value) }
func isUTC(value time.Time) bool                   { return !value.IsZero() && value.Location() == time.UTC }
func compareDecimal(left, right string) int {
	l, lok := new(big.Rat).SetString(left)
	r, rok := new(big.Rat).SetString(right)
	if !lok || !rok {
		return 0
	}
	return l.Cmp(r)
}

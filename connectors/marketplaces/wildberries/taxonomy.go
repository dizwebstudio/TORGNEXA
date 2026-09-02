package wildberries

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type wbParentCategoriesResponse struct {
	Data []wbParentCategory `json:"data"`
}

type wbParentCategory struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	IsVisible bool   `json:"isVisible"`
}

type wbSubjectsResponse struct {
	Data []wbSubject `json:"data"`
}

type wbSubject struct {
	SubjectID   int64  `json:"subjectID"`
	SubjectName string `json:"subjectName"`
	ParentID    int64  `json:"parentID"`
	ParentName  string `json:"parentName"`
	IsVisible   bool   `json:"isVisible"`
}

type wbCharacteristicsResponse struct {
	Data []wbCharacteristic `json:"data"`
}

type wbCharacteristic struct {
	CharcID   int64  `json:"charcID"`
	SubjectID int64  `json:"subjectID"`
	Name      string `json:"name"`
	Required  bool   `json:"required"`
	UnitName  string `json:"unitName"`
	CharcType int    `json:"charcType"`
}

// ReadMarketplaceListingTaxonomy reads the official WB content dictionaries
// and reduces them to the provider-neutral listing schema. Provider IDs stay
// as numeric category identity; raw response fields never leave this package.
func (connector *Connector) ReadMarketplaceListingTaxonomy(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.MarketplaceListingTaxonomyRequest) (sdk.MarketplaceListingTaxonomy, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.read") != nil || request.Validate() != nil {
		return sdk.MarketplaceListingTaxonomy{}, sdk.ErrInvalidMarketplaceListing
	}
	categoryID, err := parseWBCategoryCode(request.CategoryCode)
	if err != nil {
		return sdk.MarketplaceListingTaxonomy{}, err
	}
	var parents wbParentCategoriesResponse
	var subjects wbSubjectsResponse
	var characteristics wbCharacteristicsResponse
	err = runtime.Secrets().UseSecret(ctx, account.SecretReference, func(secret []byte) error {
		var callErr error
		parents, callErr = readWBParents(ctx, connector.transport, secret, request.Locale)
		if callErr != nil {
			return callErr
		}
		subjects, callErr = readWBSubjects(ctx, connector.transport, secret, request.Locale)
		if callErr != nil {
			return callErr
		}
		if categoryID > 0 {
			characteristics, callErr = readWBCharacteristics(ctx, connector.transport, secret, categoryID, request.Locale)
		}
		return callErr
	})
	if err != nil {
		return sdk.MarketplaceListingTaxonomy{}, err
	}

	now := connector.now().UTC()
	taxonomy := sdk.MarketplaceListingTaxonomy{
		ID:           "wb.taxonomy." + strings.ToLower(request.Locale) + "." + strconv.FormatInt(categoryID, 10),
		ChannelID:    "wildberries",
		Locale:       request.Locale,
		Jurisdiction: request.Jurisdiction,
		Version:      1,
		Source:       "wildberries.content.taxonomy.v1",
		ObservedAt:   now,
		FreshUntil:   now.Add(6 * time.Hour),
		MediaSlots:   []sdk.MarketplaceListingMediaSlot{},
	}
	parentIDs := make(map[int64]struct{}, len(parents.Data))
	for _, parent := range parents.Data {
		if parent.ID <= 0 || strings.TrimSpace(parent.Name) == "" {
			return sdk.MarketplaceListingTaxonomy{}, ErrInvalidResponse
		}
		if _, duplicate := parentIDs[parent.ID]; duplicate {
			return sdk.MarketplaceListingTaxonomy{}, ErrInvalidResponse
		}
		parentIDs[parent.ID] = struct{}{}
		taxonomy.Categories = append(taxonomy.Categories, sdk.MarketplaceListingCategory{Code: strconv.FormatInt(parent.ID, 10), Name: strings.TrimSpace(parent.Name)})
	}
	categoryIDs := make(map[int64]struct{}, len(taxonomy.Categories))
	for _, category := range taxonomy.Categories {
		id, _ := strconv.ParseInt(category.Code, 10, 64)
		categoryIDs[id] = struct{}{}
	}
	for _, subject := range subjects.Data {
		if subject.SubjectID <= 0 || strings.TrimSpace(subject.SubjectName) == "" {
			return sdk.MarketplaceListingTaxonomy{}, ErrInvalidResponse
		}
		if _, duplicate := categoryIDs[subject.SubjectID]; duplicate {
			return sdk.MarketplaceListingTaxonomy{}, ErrInvalidResponse
		}
		categoryIDs[subject.SubjectID] = struct{}{}
		parentCode := ""
		if subject.ParentID > 0 {
			if _, exists := parentIDs[subject.ParentID]; !exists {
				return sdk.MarketplaceListingTaxonomy{}, ErrInvalidResponse
			}
			parentCode = strconv.FormatInt(subject.ParentID, 10)
		}
		taxonomy.Categories = append(taxonomy.Categories, sdk.MarketplaceListingCategory{Code: strconv.FormatInt(subject.SubjectID, 10), Name: strings.TrimSpace(subject.SubjectName), ParentCode: parentCode})
	}
	if categoryID > 0 {
		if _, exists := categoryIDs[categoryID]; !exists {
			return sdk.MarketplaceListingTaxonomy{}, sdk.ErrInvalidMarketplaceListing
		}
		seenCodes := make(map[string]struct{}, len(characteristics.Data))
		for _, characteristic := range characteristics.Data {
			if characteristic.CharcID <= 0 || characteristic.SubjectID != 0 && characteristic.SubjectID != categoryID || strings.TrimSpace(characteristic.Name) == "" {
				return sdk.MarketplaceListingTaxonomy{}, ErrInvalidResponse
			}
			code := normalizedWBAttributeCode(characteristic.Name, characteristic.CharcID, seenCodes)
			seenCodes[code] = struct{}{}
			taxonomy.Attributes = append(taxonomy.Attributes, sdk.MarketplaceListingAttribute{Code: code, Name: strings.TrimSpace(characteristic.Name), ValueType: wbValueType(characteristic.CharcType), Requirement: wbRequirement(characteristic.Required), Unit: normalizedWBUnit(characteristic.UnitName), LocalizedName: map[string]string{request.Locale: strings.TrimSpace(characteristic.Name)}})
		}
		for index := range taxonomy.Categories {
			if taxonomy.Categories[index].Code == strconv.FormatInt(categoryID, 10) {
				for _, attribute := range taxonomy.Attributes {
					taxonomy.Categories[index].AttributeCodes = append(taxonomy.Categories[index].AttributeCodes, attribute.Code)
				}
				break
			}
		}
	}
	sort.Slice(taxonomy.Categories, func(i, j int) bool { return taxonomy.Categories[i].Code < taxonomy.Categories[j].Code })
	sort.Slice(taxonomy.Attributes, func(i, j int) bool { return taxonomy.Attributes[i].Code < taxonomy.Attributes[j].Code })
	fingerprint, err := taxonomy.ComputeFingerprint()
	if err != nil {
		return sdk.MarketplaceListingTaxonomy{}, ErrInvalidResponse
	}
	taxonomy.Fingerprint = fingerprint
	return taxonomy, nil
}

func readWBParents(ctx context.Context, transport Transport, secret []byte, locale string) (wbParentCategoriesResponse, error) {
	response, err := transport.Do(ctx, Request{Method: "GET", Host: contentHost, Path: "/content/v2/object/parent/all", Query: []QueryParam{{Name: "locale", Value: wbLocale(locale)}}, Token: secret})
	if err != nil {
		return wbParentCategoriesResponse{}, normalizedTransportError()
	}
	if err := normalizeHTTP(response); err != nil {
		return wbParentCategoriesResponse{}, err
	}
	var parsed wbParentCategoriesResponse
	if json.Unmarshal(response.Body, &parsed) != nil || len(parsed.Data) == 0 {
		return wbParentCategoriesResponse{}, ErrInvalidResponse
	}
	return parsed, nil
}

func readWBSubjects(ctx context.Context, transport Transport, secret []byte, locale string) (wbSubjectsResponse, error) {
	response, err := transport.Do(ctx, Request{Method: "GET", Host: contentHost, Path: "/content/v2/object/all", Query: []QueryParam{{Name: "locale", Value: wbLocale(locale)}}, Token: secret})
	if err != nil {
		return wbSubjectsResponse{}, normalizedTransportError()
	}
	if err := normalizeHTTP(response); err != nil {
		return wbSubjectsResponse{}, err
	}
	var parsed wbSubjectsResponse
	if json.Unmarshal(response.Body, &parsed) != nil || len(parsed.Data) == 0 {
		return wbSubjectsResponse{}, ErrInvalidResponse
	}
	return parsed, nil
}

func readWBCharacteristics(ctx context.Context, transport Transport, secret []byte, categoryID int64, locale string) (wbCharacteristicsResponse, error) {
	response, err := transport.Do(ctx, Request{Method: "GET", Host: contentHost, Path: "/content/v2/object/charcs/" + strconv.FormatInt(categoryID, 10), Query: []QueryParam{{Name: "locale", Value: wbLocale(locale)}}, Token: secret})
	if err != nil {
		return wbCharacteristicsResponse{}, normalizedTransportError()
	}
	if err := normalizeHTTP(response); err != nil {
		return wbCharacteristicsResponse{}, err
	}
	var parsed wbCharacteristicsResponse
	if json.Unmarshal(response.Body, &parsed) != nil {
		return wbCharacteristicsResponse{}, ErrInvalidResponse
	}
	return parsed, nil
}

func parseWBCategoryCode(value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 1 {
		return 0, sdk.ErrInvalidMarketplaceListing
	}
	return parsed, nil
}

func wbLocale(locale string) string {
	if strings.HasPrefix(strings.ToLower(locale), "en") {
		return "en"
	}
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return "zh"
	}
	return "ru"
}

func wbValueType(charcType int) sdk.MarketplaceListingValueType {
	switch charcType {
	case 1:
		return sdk.MarketplaceListingValueInteger
	case 2, 3:
		return sdk.MarketplaceListingValueDecimal
	default:
		return sdk.MarketplaceListingValueText
	}
}

func wbRequirement(required bool) sdk.MarketplaceListingRequirement {
	if required {
		return sdk.MarketplaceListingRequirementRequired
	}
	return sdk.MarketplaceListingRequirementOptional
}

func normalizedWBUnit(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	if value == "мм" {
		return "mm"
	}
	if value == "см" {
		return "cm"
	}
	if value == "м" {
		return "m"
	}
	if value == "г" {
		return "g"
	}
	if value == "кг" {
		return "kg"
	}
	if !sdkSafeCode(value) {
		return ""
	}
	return value
}

func sdkSafeCode(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for index, character := range value {
		if index == 0 && (character < 'a' || character > 'z') || index > 0 && (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' && character != '-' && character != '.' {
			return false
		}
	}
	return true
}

func normalizedWBAttributeCode(name string, charcID int64, seen map[string]struct{}) string {
	var builder strings.Builder
	for _, character := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			builder.WriteRune(character)
			continue
		}
		if builder.Len() > 0 && !strings.HasSuffix(builder.String(), "_") {
			builder.WriteByte('_')
		}
	}
	code := strings.Trim(builder.String(), "_")
	if code == "" {
		code = "charc"
	}
	if _, exists := seen[code]; exists {
		code = fmt.Sprintf("%s_%d", code, charcID)
	}
	return code
}

var _ sdk.MarketplaceListingTaxonomyReader = (*Connector)(nil)

package ozon

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type ozonCategoryTreeRequest struct {
	CategoryID int64  `json:"category_id,omitempty"`
	Language   string `json:"language"`
}

type ozonCategoryTreeResponse struct {
	Result []ozonCategoryNode `json:"result"`
}

type ozonCategoryNode struct {
	CategoryID int64              `json:"category_id"`
	Title      string             `json:"title"`
	Children   []ozonCategoryNode `json:"children"`
}

type ozonCategoryAttributeRequest struct {
	AttributeType string  `json:"attribute_type"`
	CategoryIDs   []int64 `json:"category_id"`
	Language      string  `json:"language"`
}

type ozonCategoryAttributeResponse struct {
	Result []struct {
		Attributes []ozonCategoryAttribute `json:"attributes"`
	} `json:"result"`
}

type ozonCategoryAttribute struct {
	CategoryDependent bool   `json:"category_dependent"`
	DictionaryID      int64  `json:"dictionary_id"`
	ID                int64  `json:"id"`
	IsRequired        bool   `json:"is_required"`
	Name              string `json:"name"`
	Type              string `json:"type"`
}

type ozonAttributeValuesRequest struct {
	AttributeID int64  `json:"attribute_id"`
	CategoryID  int64  `json:"category_id"`
	Language    string `json:"language"`
	LastValueID int64  `json:"last_value_id,omitempty"`
	Limit       int    `json:"limit"`
}

type ozonAttributeValuesResponse struct {
	HasNext bool `json:"has_next"`
	Result  []struct {
		ID    int64  `json:"id"`
		Value string `json:"value"`
	} `json:"result"`
}

// ReadMarketplaceListingTaxonomy reads Ozon's category tree and attribute
// dictionaries through the Seller API and returns only the normalized listing
// contract. The selected category keeps dictionary reads bounded and avoids
// pretending that a partial provider schema is complete.
func (connector *Connector) ReadMarketplaceListingTaxonomy(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.MarketplaceListingTaxonomyRequest) (sdk.MarketplaceListingTaxonomy, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "products.read") != nil || request.Validate() != nil {
		return sdk.MarketplaceListingTaxonomy{}, sdk.ErrInvalidMarketplaceListing
	}
	categoryID, err := parseOzonCategoryCode(request.CategoryCode)
	if err != nil {
		return sdk.MarketplaceListingTaxonomy{}, err
	}
	var tree ozonCategoryTreeResponse
	var attributes []ozonCategoryAttribute
	var enumValues map[int64][]sdk.MarketplaceListingEnumValue
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(clientID, apiKey []byte) error {
		var callErr error
		tree, callErr = readOzonCategoryTree(ctx, connector.transport, clientID, apiKey, categoryID)
		if callErr != nil {
			return callErr
		}
		if categoryID == 0 {
			return nil
		}
		attributes, callErr = readOzonCategoryAttributes(ctx, connector.transport, clientID, apiKey, categoryID)
		if callErr != nil {
			return callErr
		}
		enumValues = make(map[int64][]sdk.MarketplaceListingEnumValue)
		for _, attribute := range attributes {
			if attribute.DictionaryID == 0 {
				continue
			}
			values, valuesErr := readOzonAttributeValues(ctx, connector.transport, clientID, apiKey, categoryID, attribute.ID)
			if valuesErr != nil {
				return valuesErr
			}
			enumValues[attribute.ID] = values
		}
		return nil
	})
	if err != nil {
		return sdk.MarketplaceListingTaxonomy{}, err
	}
	now := connector.now().UTC()
	taxonomy := sdk.MarketplaceListingTaxonomy{ID: "ozon.taxonomy." + strings.ToLower(request.Locale) + "." + strconv.FormatInt(categoryID, 10), ChannelID: "ozon", Locale: request.Locale, Jurisdiction: request.Jurisdiction, Version: 1, Source: "ozon.seller.taxonomy.v1", ObservedAt: now, FreshUntil: now.Add(6 * time.Hour), MediaSlots: []sdk.MarketplaceListingMediaSlot{}}
	if err := flattenOzonCategories(tree.Result, "", &taxonomy.Categories); err != nil {
		return sdk.MarketplaceListingTaxonomy{}, err
	}
	if categoryID > 0 {
		seenCodes := make(map[string]struct{}, len(attributes))
		for _, attribute := range attributes {
			if attribute.ID <= 0 || strings.TrimSpace(attribute.Name) == "" {
				return sdk.MarketplaceListingTaxonomy{}, ErrInvalidResponse
			}
			code := "ozon.attribute." + strconv.FormatInt(attribute.ID, 10)
			if _, exists := seenCodes[code]; exists {
				return sdk.MarketplaceListingTaxonomy{}, ErrInvalidResponse
			}
			seenCodes[code] = struct{}{}
			definition := sdk.MarketplaceListingAttribute{Code: code, Name: strings.TrimSpace(attribute.Name), ValueType: ozonValueType(attribute.Type, attribute.DictionaryID > 0), Requirement: ozonRequirement(attribute.IsRequired), LocalizedName: map[string]string{request.Locale: strings.TrimSpace(attribute.Name)}}
			if values := enumValues[attribute.ID]; len(values) > 0 {
				definition.EnumValues = values
				definition.ValueType = sdk.MarketplaceListingValueEnum
			}
			taxonomy.Attributes = append(taxonomy.Attributes, definition)
		}
		categoryCode := strconv.FormatInt(categoryID, 10)
		for index := range taxonomy.Categories {
			if taxonomy.Categories[index].Code == categoryCode {
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

func readOzonCategoryTree(ctx context.Context, transport Transport, clientID, apiKey []byte, categoryID int64) (ozonCategoryTreeResponse, error) {
	body, err := json.Marshal(ozonCategoryTreeRequest{CategoryID: categoryID, Language: "DEFAULT"})
	if err != nil {
		return ozonCategoryTreeResponse{}, ErrInvalidResponse
	}
	response, callErr := transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: "/v1/category/tree", Body: body, ClientID: clientID, APIKey: apiKey})
	if callErr != nil {
		return ozonCategoryTreeResponse{}, normalizedTransportError()
	}
	if err := normalizeHTTP(response); err != nil {
		return ozonCategoryTreeResponse{}, err
	}
	var parsed ozonCategoryTreeResponse
	if len(response.Body) == 0 || len(response.Body) > maxBodyBytes || json.Unmarshal(response.Body, &parsed) != nil || len(parsed.Result) == 0 {
		return ozonCategoryTreeResponse{}, ErrInvalidResponse
	}
	return parsed, nil
}

func readOzonCategoryAttributes(ctx context.Context, transport Transport, clientID, apiKey []byte, categoryID int64) ([]ozonCategoryAttribute, error) {
	body, err := json.Marshal(ozonCategoryAttributeRequest{AttributeType: "ALL", CategoryIDs: []int64{categoryID}, Language: "DEFAULT"})
	if err != nil {
		return nil, ErrInvalidResponse
	}
	response, callErr := transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: "/v3/category/attribute", Body: body, ClientID: clientID, APIKey: apiKey})
	if callErr != nil {
		return nil, normalizedTransportError()
	}
	if err := normalizeHTTP(response); err != nil {
		return nil, err
	}
	var parsed ozonCategoryAttributeResponse
	if len(response.Body) == 0 || len(response.Body) > maxBodyBytes || json.Unmarshal(response.Body, &parsed) != nil || len(parsed.Result) != 1 {
		return nil, ErrInvalidResponse
	}
	return parsed.Result[0].Attributes, nil
}

func readOzonAttributeValues(ctx context.Context, transport Transport, clientID, apiKey []byte, categoryID, attributeID int64) ([]sdk.MarketplaceListingEnumValue, error) {
	values := make([]sdk.MarketplaceListingEnumValue, 0, 32)
	var lastID int64
	for page := 0; page < 256; page++ {
		body, err := json.Marshal(ozonAttributeValuesRequest{AttributeID: attributeID, CategoryID: categoryID, Language: "DEFAULT", LastValueID: lastID, Limit: 256})
		if err != nil {
			return nil, ErrInvalidResponse
		}
		response, callErr := transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: "/v2/category/attribute/values", Body: body, ClientID: clientID, APIKey: apiKey})
		if callErr != nil {
			return nil, normalizedTransportError()
		}
		if err := normalizeHTTP(response); err != nil {
			return nil, err
		}
		var parsed ozonAttributeValuesResponse
		if len(response.Body) == 0 || len(response.Body) > maxBodyBytes || json.Unmarshal(response.Body, &parsed) != nil || len(parsed.Result) == 0 {
			return nil, ErrInvalidResponse
		}
		for _, item := range parsed.Result {
			if item.ID <= 0 || strings.TrimSpace(item.Value) == "" || len(values) >= 256 {
				return nil, ErrInvalidResponse
			}
			values = append(values, sdk.MarketplaceListingEnumValue{Code: "value_" + strconv.FormatInt(item.ID, 10), Label: strings.TrimSpace(item.Value)})
			lastID = item.ID
		}
		if !parsed.HasNext {
			return values, nil
		}
		if lastID == 0 {
			return nil, ErrInvalidResponse
		}
	}
	return nil, ErrInvalidResponse
}

func flattenOzonCategories(nodes []ozonCategoryNode, parent string, result *[]sdk.MarketplaceListingCategory) error {
	for _, node := range nodes {
		if node.CategoryID <= 0 || strings.TrimSpace(node.Title) == "" {
			return ErrInvalidResponse
		}
		code := strconv.FormatInt(node.CategoryID, 10)
		*result = append(*result, sdk.MarketplaceListingCategory{Code: code, Name: strings.TrimSpace(node.Title), ParentCode: parent})
		if err := flattenOzonCategories(node.Children, code, result); err != nil {
			return err
		}
	}
	return nil
}

func parseOzonCategoryCode(value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 1 {
		return 0, sdk.ErrInvalidMarketplaceListing
	}
	return parsed, nil
}

func ozonValueType(value string, dictionary bool) sdk.MarketplaceListingValueType {
	if dictionary {
		return sdk.MarketplaceListingValueEnum
	}
	switch strings.ToUpper(value) {
	case "INTEGER", "INT", "TYPE_INT":
		return sdk.MarketplaceListingValueInteger
	case "DOUBLE", "FLOAT", "NUMBER", "TYPE_DOUBLE":
		return sdk.MarketplaceListingValueDecimal
	case "BOOLEAN", "BOOL", "TYPE_BOOL":
		return sdk.MarketplaceListingValueBoolean
	case "DATE", "TYPE_DATE":
		return sdk.MarketplaceListingValueDate
	default:
		return sdk.MarketplaceListingValueText
	}
}

func ozonRequirement(required bool) sdk.MarketplaceListingRequirement {
	if required {
		return sdk.MarketplaceListingRequirementRequired
	}
	return sdk.MarketplaceListingRequirementOptional
}

var _ sdk.MarketplaceListingTaxonomyReader = (*Connector)(nil)

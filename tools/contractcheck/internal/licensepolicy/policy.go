// Package licensepolicy evaluates dependency SPDX expressions using a
// checked-in default-deny policy.
package licensepolicy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
)

const (
	maxPolicyBytes     = 1 << 20
	maxReportBytes     = 32 << 20
	maxExpressionBytes = 512
	maxTokens          = 128
	maxDepth           = 32
)

var identifierRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+-]*$`)

// Selection records the approved branch of one exact SPDX OR expression.
type Selection struct {
	Expression string `json:"expression"`
	Selected   string `json:"selected"`
}

// Policy is the strict on-disk license policy.
type Policy struct {
	Version              int         `json:"version"`
	AllowedSPDX          []string    `json:"allowed_spdx"`
	ReviewRequiredSPDX   []string    `json:"review_required_spdx"`
	DeniedSPDX           []string    `json:"denied_spdx"`
	SelectedORChoices    []Selection `json:"selected_or_choices"`
	UnknownLicensePolicy string      `json:"unknown_license_policy"`
}

// Result is safe machine-readable evaluation output.
type Result struct {
	Expressions int `json:"expressions"`
}

type trivyReport struct {
	SchemaVersion int           `json:"SchemaVersion"`
	Results       []trivyResult `json:"Results"`
}

type trivyResult struct {
	Licenses []trivyLicense `json:"Licenses"`
}

type trivyLicense struct {
	Name string `json:"Name"`
}

// ValidatePolicy validates policy syntax and invariants without a report.
func ValidatePolicy(data []byte) error {
	var policy Policy
	if err := decodeStrict(data, &policy); err != nil {
		return fmt.Errorf("decode policy: %w", err)
	}
	_, err := compilePolicy(policy)
	return err
}

// CheckFiles loads and validates a policy and one sanitized Trivy report.
func CheckFiles(policyPath, reportPath string) (Result, error) {
	policyData, err := readBounded(policyPath, maxPolicyBytes)
	if err != nil {
		return Result{}, fmt.Errorf("read policy: %w", err)
	}
	reportData, err := readBounded(reportPath, maxReportBytes)
	if err != nil {
		return Result{}, fmt.Errorf("read report: %w", err)
	}
	return Check(policyData, reportData)
}

// Check validates every license expression in a sanitized Trivy report.
func Check(policyData, reportData []byte) (Result, error) {
	var policy Policy
	if err := decodeStrict(policyData, &policy); err != nil {
		return Result{}, fmt.Errorf("decode policy: %w", err)
	}
	compiled, err := compilePolicy(policy)
	if err != nil {
		return Result{}, err
	}
	var report trivyReport
	if err := decodeScannerJSON(reportData, &report); err != nil {
		return Result{}, fmt.Errorf("decode Trivy report: %w", err)
	}
	if report.SchemaVersion != 2 || report.Results == nil {
		return Result{}, fmt.Errorf("Trivy license report must use schema version 2 with a results array")
	}
	count := 0
	for _, result := range report.Results {
		for _, license := range result.Licenses {
			count++
			if err := compiled.evaluate(license.Name); err != nil {
				return Result{}, fmt.Errorf("license expression %q: %w", license.Name, err)
			}
		}
	}
	return Result{Expressions: count}, nil
}

func decodeScannerJSON(data []byte, target any) error {
	if err := rejectDuplicateKeys(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return fmt.Errorf("multiple JSON values")
	} else if err != io.EOF {
		return fmt.Errorf("trailing JSON data: %w", err)
	}
	return nil
}

type compiledPolicy struct {
	allowed    map[string]struct{}
	review     map[string]struct{}
	denied     map[string]struct{}
	selections map[string]string
}

func compilePolicy(policy Policy) (compiledPolicy, error) {
	if policy.Version != 1 {
		return compiledPolicy{}, fmt.Errorf("license policy version must be 1")
	}
	if policy.UnknownLicensePolicy != "deny" {
		return compiledPolicy{}, fmt.Errorf("unknown_license_policy must be deny")
	}
	if policy.AllowedSPDX == nil || policy.ReviewRequiredSPDX == nil || policy.DeniedSPDX == nil || policy.SelectedORChoices == nil {
		return compiledPolicy{}, fmt.Errorf("license policy arrays must be present and non-null")
	}
	compiled := compiledPolicy{
		allowed: make(map[string]struct{}), review: make(map[string]struct{}),
		denied: make(map[string]struct{}), selections: make(map[string]string),
	}
	all := make(map[string]string)
	for field, values := range map[string][]string{
		"allowed_spdx": policy.AllowedSPDX, "review_required_spdx": policy.ReviewRequiredSPDX, "denied_spdx": policy.DeniedSPDX,
	} {
		if !sort.StringsAreSorted(values) {
			return compiledPolicy{}, fmt.Errorf("%s must be sorted", field)
		}
		for index, value := range values {
			if !identifierRE.MatchString(value) || value == "UNKNOWN" || value == "NOASSERTION" {
				return compiledPolicy{}, fmt.Errorf("%s contains invalid SPDX identifier %q", field, value)
			}
			if index > 0 && values[index-1] == value {
				return compiledPolicy{}, fmt.Errorf("%s contains duplicate %q", field, value)
			}
			if previous, exists := all[value]; exists {
				return compiledPolicy{}, fmt.Errorf("SPDX identifier %q appears in both %s and %s", value, previous, field)
			}
			all[value] = field
			switch field {
			case "allowed_spdx":
				compiled.allowed[value] = struct{}{}
			case "review_required_spdx":
				compiled.review[value] = struct{}{}
			case "denied_spdx":
				compiled.denied[value] = struct{}{}
			}
		}
	}
	previous := ""
	for _, selection := range policy.SelectedORChoices {
		if selection.Expression <= previous && previous != "" {
			return compiledPolicy{}, fmt.Errorf("selected_or_choices must be strictly sorted by expression")
		}
		previous = selection.Expression
		if _, exists := compiled.selections[selection.Expression]; exists {
			return compiledPolicy{}, fmt.Errorf("duplicate selected OR expression %q", selection.Expression)
		}
		if _, allowed := compiled.allowed[selection.Selected]; !allowed {
			return compiledPolicy{}, fmt.Errorf("selected OR choice %q is not allowed", selection.Selected)
		}
		node, err := parseExpression(selection.Expression)
		if err != nil || !node.hasOR() || !node.contains(selection.Selected) {
			return compiledPolicy{}, fmt.Errorf("selected OR expression %q does not contain allowed choice %q", selection.Expression, selection.Selected)
		}
		compiled.selections[selection.Expression] = selection.Selected
	}
	return compiled, nil
}

func (policy compiledPolicy) evaluate(expression string) error {
	expression = normalizeLicenseExpression(expression)
	node, err := parseExpression(expression)
	if err != nil {
		return err
	}
	if node.hasOR() {
		selected, exists := policy.selections[expression]
		if !exists {
			return fmt.Errorf("OR expression requires an explicit selected_or_choices entry")
		}
		return policy.evaluateSelected(node, selected)
	}
	for _, identifier := range node.identifiers() {
		if err := policy.evaluateAtom(identifier); err != nil {
			return err
		}
	}
	return nil
}

// normalizeLicenseExpression maps the exact human-readable alias emitted by
// Trivy to the repository's SPDX-style policy token. Other malformed or
// unknown expressions remain fail-closed in parseExpression/evaluateAtom.
func normalizeLicenseExpression(expression string) string {
	if strings.TrimSpace(expression) == "Public Domain" {
		return "Public-Domain"
	}
	return expression
}

func (policy compiledPolicy) evaluateSelected(node *expressionNode, selected string) error {
	if node == nil {
		return fmt.Errorf("selected OR choice %q is not present", selected)
	}
	if node.identifier != "" {
		return policy.evaluateAtom(node.identifier)
	}
	if node.op == "AND" {
		if err := policy.evaluateSelected(node.left, selected); err != nil {
			return err
		}
		return policy.evaluateSelected(node.right, selected)
	}
	leftContains, rightContains := node.left.contains(selected), node.right.contains(selected)
	if leftContains == rightContains {
		return fmt.Errorf("selected OR choice %q is missing or ambiguous", selected)
	}
	if leftContains {
		return policy.evaluateAll(node.left)
	}
	return policy.evaluateAll(node.right)
}

func (policy compiledPolicy) evaluateAll(node *expressionNode) error {
	if node == nil {
		return fmt.Errorf("invalid empty SPDX branch")
	}
	if node.identifier != "" {
		return policy.evaluateAtom(node.identifier)
	}
	if node.op == "OR" {
		return fmt.Errorf("nested OR expressions require separate explicit choices and are not supported")
	}
	if err := policy.evaluateAll(node.left); err != nil {
		return err
	}
	return policy.evaluateAll(node.right)
}

func (policy compiledPolicy) evaluateAtom(identifier string) error {
	if _, allowed := policy.allowed[identifier]; allowed {
		return nil
	}
	if _, review := policy.review[identifier]; review {
		return fmt.Errorf("SPDX identifier %q requires legal review", identifier)
	}
	if _, denied := policy.denied[identifier]; denied {
		return fmt.Errorf("SPDX identifier %q is denied", identifier)
	}
	return fmt.Errorf("SPDX identifier %q is unknown and denied by default", identifier)
}

type expressionNode struct {
	op          string
	identifier  string
	left, right *expressionNode
}

func (node *expressionNode) hasOR() bool {
	return node != nil && (node.op == "OR" || node.left.hasOR() || node.right.hasOR())
}

func (node *expressionNode) contains(identifier string) bool {
	return node != nil && (node.identifier == identifier || node.left.contains(identifier) || node.right.contains(identifier))
}

func (node *expressionNode) identifiers() []string {
	if node == nil {
		return nil
	}
	if node.identifier != "" {
		return []string{node.identifier}
	}
	return append(node.left.identifiers(), node.right.identifiers()...)
}

type tokenStream struct {
	tokens []string
	index  int
}

func parseExpression(value string) (*expressionNode, error) {
	if value != strings.TrimSpace(value) || len(value) == 0 || len(value) > maxExpressionBytes {
		return nil, fmt.Errorf("SPDX expression must be 1..%d trimmed bytes", maxExpressionBytes)
	}
	tokens, err := tokenize(value)
	if err != nil {
		return nil, err
	}
	stream := &tokenStream{tokens: tokens}
	node, err := stream.parseOR(0)
	if err != nil {
		return nil, err
	}
	if stream.index != len(stream.tokens) {
		return nil, fmt.Errorf("unexpected SPDX token %q", stream.tokens[stream.index])
	}
	return node, nil
}

func tokenize(value string) ([]string, error) {
	value = strings.NewReplacer("(", " ( ", ")", " ) ").Replace(value)
	tokens := strings.Fields(value)
	if len(tokens) == 0 || len(tokens) > maxTokens {
		return nil, fmt.Errorf("SPDX expression token count is out of bounds")
	}
	for _, token := range tokens {
		if token != "AND" && token != "OR" && token != "WITH" && token != "(" && token != ")" && !identifierRE.MatchString(token) {
			return nil, fmt.Errorf("invalid SPDX token %q", token)
		}
	}
	return tokens, nil
}

func (stream *tokenStream) parseOR(depth int) (*expressionNode, error) {
	left, err := stream.parseAND(depth)
	if err != nil {
		return nil, err
	}
	for stream.peek() == "OR" {
		stream.index++
		right, err := stream.parseAND(depth)
		if err != nil {
			return nil, err
		}
		left = &expressionNode{op: "OR", left: left, right: right}
	}
	return left, nil
}

func (stream *tokenStream) parseAND(depth int) (*expressionNode, error) {
	left, err := stream.parsePrimary(depth)
	if err != nil {
		return nil, err
	}
	for stream.peek() == "AND" {
		stream.index++
		right, err := stream.parsePrimary(depth)
		if err != nil {
			return nil, err
		}
		left = &expressionNode{op: "AND", left: left, right: right}
	}
	return left, nil
}

func (stream *tokenStream) parsePrimary(depth int) (*expressionNode, error) {
	if depth > maxDepth || stream.index >= len(stream.tokens) {
		return nil, fmt.Errorf("invalid or excessively nested SPDX expression")
	}
	token := stream.tokens[stream.index]
	if token == "(" {
		stream.index++
		node, err := stream.parseOR(depth + 1)
		if err != nil {
			return nil, err
		}
		if stream.peek() != ")" {
			return nil, fmt.Errorf("unclosed SPDX parenthesis")
		}
		stream.index++
		return node, nil
	}
	if token == "WITH" || token == "AND" || token == "OR" || token == ")" || !identifierRE.MatchString(token) {
		return nil, fmt.Errorf("expected SPDX identifier, got %q", token)
	}
	stream.index++
	if stream.peek() == "WITH" {
		return nil, fmt.Errorf("SPDX WITH exceptions require an explicit reviewed policy and are not supported")
	}
	return &expressionNode{identifier: token}, nil
}

func (stream *tokenStream) peek() string {
	if stream.index >= len(stream.tokens) {
		return ""
	}
	return stream.tokens[stream.index]
}

func readBounded(path string, maximum int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maximum {
		return nil, fmt.Errorf("must be a non-symlink regular file no larger than %d bytes", maximum)
	}
	// #nosec G304 -- caller supplies repository/temp paths; Lstat rejects symlinks and bounds size.
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("file grew beyond %d bytes while reading", maximum)
	}
	return data, nil
}

func decodeStrict(data []byte, target any) error {
	if err := rejectDuplicateKeys(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return fmt.Errorf("multiple JSON values")
	} else if err != io.EOF {
		return fmt.Errorf("trailing JSON data: %w", err)
	}
	return nil
}

func rejectDuplicateKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var walk func(int) error
	walk = func(depth int) error {
		if depth > maxDepth {
			return fmt.Errorf("JSON nesting exceeds %d", maxDepth)
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := make(map[string]struct{})
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return fmt.Errorf("JSON object key is not a string")
				}
				if _, exists := seen[key]; exists {
					return fmt.Errorf("duplicate JSON object key %q", key)
				}
				seen[key] = struct{}{}
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		case '[':
			for decoder.More() {
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		default:
			return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
		}
	}
	if err := walk(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

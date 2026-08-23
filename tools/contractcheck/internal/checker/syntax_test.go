package checker

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestParseStrictJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "object", input: `{"value":1}`},
		{name: "duplicate key", input: `{"value":1,"value":2}`, wantErr: "duplicate object key"},
		{name: "trailing value", input: `{} []`, wantErr: "multiple JSON values"},
		{name: "trailing garbage", input: `{} broken`, wantErr: "trailing data"},
		{name: "too deep", input: strings.Repeat("[", maxDocumentDepth+2) + strings.Repeat("]", maxDocumentDepth+2), wantErr: "nesting exceeds"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseStrictJSON([]byte(test.input))
			assertErrorContains(t, err, test.wantErr)
		})
	}
}

func TestParseStrictJSONPreservesEmptyArray(t *testing.T) {
	t.Parallel()
	value, err := parseStrictJSON([]byte(`[]`))
	if err != nil {
		t.Fatalf("parse empty array: %v", err)
	}
	if !reflect.DeepEqual(value, []any{}) {
		t.Fatalf("empty array changed representation: %#v", value)
	}
}

func TestParseStrictJSONRejectsInvalidUTF8(t *testing.T) {
	t.Parallel()
	_, err := parseStrictJSON([]byte{0xff})
	assertErrorContains(t, err, "UTF-8")
}

func TestParseStrictJSONNodeBudgets(t *testing.T) {
	t.Parallel()
	wide := "[" + strings.Repeat("0,", maxJSONDocumentNodes) + "0]"
	_, err := parseStrictJSON([]byte(wide))
	if !errors.Is(err, errJSONDocumentNodeLimit) {
		t.Fatalf("wide JSON error = %v, want document node limit", err)
	}

	totalRemaining := 2
	_, err = parseStrictJSONWithBudget(context.Background(), []byte(`[0,1]`), &totalRemaining)
	if !errors.Is(err, errJSONTotalNodeLimit) {
		t.Fatalf("aggregate JSON error = %v, want total node limit", err)
	}
}

func TestParseStrictJSONChecksContextWithinDocument(t *testing.T) {
	t.Parallel()
	ctx := newCancelAfterContext(50)
	totalRemaining := maxJSONTotalNodes
	wide := "[" + strings.Repeat("0,", 1_000) + "0]"
	_, err := parseStrictJSONWithBudget(ctx, []byte(wide), &totalRemaining)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled parse error = %v, want context.Canceled", err)
	}
}

func TestParseStrictYAML(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "document", input: "openapi: 3.1.0\ninfo:\n  title: test\n"},
		{name: "duplicate key", input: "value: 1\nvalue: 2\n", wantErr: "duplicate mapping key"},
		{name: "anchor", input: "base: &base value\n", wantErr: "aliases and anchors"},
		{name: "alias", input: "base: &base value\ncopy: *base\n", wantErr: "aliases and anchors"},
		{name: "merge key", input: "value:\n  <<: {a: 1}\n", wantErr: "merge keys"},
		{name: "custom short tag", input: "value: !danger payload\n", wantErr: "tag"},
		{name: "custom URI tag", input: "value: !<tag:example.com,2026:danger> payload\n", wantErr: "tag"},
		{name: "binary tag", input: "value: !!binary cGF5bG9hZA==\n", wantErr: "tag"},
		{name: "multiple documents", input: "value: 1\n---\nvalue: 2\n", wantErr: "multiple YAML documents"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseStrictYAML([]byte(test.input))
			assertErrorContains(t, err, test.wantErr)
		})
	}
}

func TestParseComposeYAMLAllowsAnchorsWithoutWeakeningSyntaxChecks(t *testing.T) {
	t.Parallel()
	remaining := maxYAMLTotalNodes
	_, err := parseComposeYAMLWithBudget(context.Background(), []byte("x-common: &common\n  image: postgres:18-alpine@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\nservices:\n  postgres:\n    <<: *common\n"), &remaining)
	if err != nil {
		t.Fatalf("valid Compose anchor rejected: %v", err)
	}

	remaining = maxYAMLTotalNodes
	_, err = parseComposeYAMLWithBudget(context.Background(), []byte("services:\n  unsafe: !danger value\n"), &remaining)
	assertErrorContains(t, err, "tag")
}

func TestParseStrictYAMLNodeBudgetsAndContext(t *testing.T) {
	t.Parallel()
	totalRemaining := 2
	_, err := parseStrictYAMLWithBudget(context.Background(), []byte("value: test\n"), &totalRemaining)
	if !errors.Is(err, errYAMLTotalNodeLimit) {
		t.Fatalf("aggregate YAML error = %v, want total node limit", err)
	}

	scalar := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "value"}
	sequence := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: make([]*yaml.Node, maxYAMLNodes)}
	for index := range sequence.Content {
		sequence.Content[index] = scalar
	}
	count := 0
	totalRemaining = maxYAMLTotalNodes
	err = validateYAMLNode(context.Background(), sequence, 0, &count, &totalRemaining)
	assertErrorContains(t, err, "document exceeds")

	ctx := newCancelAfterContext(5)
	totalRemaining = maxYAMLTotalNodes
	_, err = parseStrictYAMLWithBudget(ctx, []byte("root:\n  child: value\n"), &totalRemaining)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled YAML error = %v, want context.Canceled", err)
	}
}

func TestDiagnosticsAreStableBoundedAndSanitized(t *testing.T) {
	t.Parallel()
	var problems diagnostics
	problems.add("z\npath", "bad\x1bvalue")
	problems.add("a", "%s", strings.Repeat("x", maxDiagnosticLength+100))
	problems.add("z\npath", "bad\x1bvalue")
	err := problems.err()
	if err == nil {
		t.Fatal("expected diagnostics")
	}
	message := err.Error()
	if strings.ContainsAny(message, "\x1b\r") || strings.Contains(message, "z\npath") {
		t.Fatalf("diagnostic contains unsanitized controls: %q", message)
	}
	if !strings.Contains(message, `z\u000Apath`) || !strings.Contains(message, `bad\u001Bvalue`) {
		t.Fatalf("diagnostic did not escape controls: %q", message)
	}
	if strings.Count(message, "bad\\u001Bvalue") != 1 {
		t.Fatalf("duplicate diagnostic was not collapsed: %q", message)
	}
	if len(message) > maxDiagnosticLength+500 {
		t.Fatalf("diagnostic was not bounded: %d bytes", len(message))
	}
	if strings.Index(message, "a: ") > strings.Index(message, "z\\u000Apath: ") {
		t.Fatalf("diagnostics are not sorted: %q", message)
	}
	if repeated := problems.err().Error(); repeated != message {
		t.Fatalf("diagnostics changed between reads:\nfirst:  %q\nsecond: %q", message, repeated)
	}
}

func FuzzParseStrictJSONDoesNotPanic(f *testing.F) {
	f.Add([]byte(`{"value":1}`))
	f.Add([]byte(`{"value":1,"value":2}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = parseStrictJSON(data)
	})
}

func FuzzParseStrictYAMLDoesNotPanic(f *testing.F) {
	f.Add([]byte("value: test\n"))
	f.Add([]byte("value: &anchor test\ncopy: *anchor\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = parseStrictYAML(data)
	})
}

func assertErrorContains(t *testing.T, err error, want string) {
	t.Helper()
	if want == "" {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return
	}
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want substring %q", err, want)
	}
}

type cancelAfterContext struct {
	calls  int
	limit  int
	done   chan struct{}
	closed bool
}

func newCancelAfterContext(limit int) *cancelAfterContext {
	return &cancelAfterContext{limit: limit, done: make(chan struct{})}
}

func (c *cancelAfterContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *cancelAfterContext) Done() <-chan struct{}       { return c.done }
func (c *cancelAfterContext) Value(any) any               { return nil }

func (c *cancelAfterContext) Err() error {
	c.calls++
	if c.calls < c.limit {
		return nil
	}
	if !c.closed {
		close(c.done)
		c.closed = true
	}
	return context.Canceled
}

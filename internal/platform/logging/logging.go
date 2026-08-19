// Package logging creates structured, redacting process loggers.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"reflect"

	"github.com/torgnexa/torgnexa/internal/platform/privacy"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

const (
	redactedValue        = secrets.RedactedValue
	redactedComplexValue = "[REDACTED_COMPLEX_VALUE]"
)

// Options controls the process log handler.
type Options struct {
	Level     string
	Format    string
	AddSource bool
}

// New returns a structured logger that redacts attributes with sensitive keys.
func New(output io.Writer, opts Options) (*slog.Logger, error) {
	if output == nil || isNil(output) {
		return nil, fmt.Errorf("log output is required")
	}
	level, err := parseLevel(opts.Level)
	if err != nil {
		return nil, err
	}

	handlerOptions := &slog.HandlerOptions{
		Level:     level,
		AddSource: opts.AddSource,
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.TimeKey && attr.Value.Kind() == slog.KindTime {
				attr.Value = slog.TimeValue(attr.Value.Time().UTC())
			}
			return attr
		},
	}
	var handler slog.Handler
	switch opts.Format {
	case "json":
		handler = slog.NewJSONHandler(output, handlerOptions)
	case "text":
		handler = slog.NewTextHandler(output, handlerOptions)
	default:
		return nil, fmt.Errorf("unsupported log format %q", opts.Format)
	}
	return slog.New(redactingHandler{next: handler}), nil
}

func isNil(value any) bool {
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func parseLevel(raw string) (slog.Level, error) {
	switch raw {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unsupported log level %q", raw)
	}
}

type redactingHandler struct {
	next         slog.Handler
	redactAll    bool
	redactMarker string
}

func (h redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h redactingHandler) Handle(ctx context.Context, record slog.Record) error {
	redacted := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		if h.redactAll {
			redacted.AddAttrs(redactEntireAttr(attr, h.redactMarker))
		} else {
			redacted.AddAttrs(redactAttr(attr))
		}
		return true
	})
	return h.next.Handle(ctx, redacted)
}

func (h redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, len(attrs))
	for i, attr := range attrs {
		if h.redactAll {
			redacted[i] = redactEntireAttr(attr, h.redactMarker)
		} else {
			redacted[i] = redactAttr(attr)
		}
	}
	return redactingHandler{next: h.next.WithAttrs(redacted), redactAll: h.redactAll, redactMarker: h.redactMarker}
}

func (h redactingHandler) WithGroup(name string) slog.Handler {
	marker, sensitive := privacy.RedactionForKey(name)
	if h.redactAll {
		marker = h.redactMarker
	}
	return redactingHandler{next: h.next.WithGroup(name), redactAll: h.redactAll || sensitive, redactMarker: marker}
}

func redactAttr(attr slog.Attr) slog.Attr {
	attr.Value = attr.Value.Resolve()
	if marker, sensitive := privacy.RedactionForKey(attr.Key); sensitive {
		return slog.String(attr.Key, marker)
	}
	if attr.Value.Kind() != slog.KindGroup {
		if attr.Value.Kind() == slog.KindString {
			value := attr.Value.String()
			if redacted := privacy.RedactString(attr.Key, value); redacted != value {
				return slog.String(attr.Key, redacted)
			}
		}
		if attr.Value.Kind() == slog.KindAny {
			value := attr.Value.Any()
			if err, ok := value.(error); ok {
				return slog.String(attr.Key, fmt.Sprintf("%T", err))
			}
			if scalar, ok := namedScalarAttr(attr.Key, value); ok {
				return scalar
			}
			// Raw maps, structs, slices, headers, and other arbitrary objects are
			// never safe log contracts. Callers must emit allowlisted scalar attrs
			// or slog.Group values instead.
			return slog.String(attr.Key, redactedComplexValue)
		}
		return attr
	}
	children := attr.Value.Group()
	redacted := make([]slog.Attr, len(children))
	for i, child := range children {
		redacted[i] = redactAttr(child)
	}
	return slog.Attr{Key: attr.Key, Value: slog.GroupValue(redacted...)}
}

func redactEntireAttr(attr slog.Attr, marker string) slog.Attr {
	if marker == "" {
		marker = redactedValue
	}
	attr.Value = attr.Value.Resolve()
	if attr.Value.Kind() != slog.KindGroup {
		return slog.String(attr.Key, marker)
	}
	children := attr.Value.Group()
	redacted := make([]slog.Attr, len(children))
	for i, child := range children {
		redacted[i] = redactEntireAttr(child, marker)
	}
	return slog.Attr{Key: attr.Key, Value: slog.GroupValue(redacted...)}
}

func namedScalarAttr(key string, value any) (slog.Attr, bool) {
	reflected := reflect.ValueOf(value)
	if !reflected.IsValid() {
		return slog.Any(key, nil), true
	}
	switch reflected.Kind() {
	case reflect.String:
		return slog.String(key, reflected.String()), true
	case reflect.Bool:
		return slog.Bool(key, reflected.Bool()), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return slog.Int64(key, reflected.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return slog.Uint64(key, reflected.Uint()), true
	case reflect.Float32, reflect.Float64:
		return slog.Float64(key, reflected.Float()), true
	default:
		return slog.Attr{}, false
	}
}

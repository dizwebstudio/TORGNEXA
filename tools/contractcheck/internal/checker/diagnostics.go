package checker

import (
	"container/heap"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxDiagnostics      = 1_000
	maxDiagnosticLength = 4_096
)

type diagnostics struct {
	items   diagnosticMaxHeap
	omitted bool
	seen    map[string]struct{}
}

func (d *diagnostics) add(path, format string, args ...any) {
	message := sanitizeDiagnostic(fmt.Sprintf(format, args...))
	if path != "" {
		message = sanitizeDiagnostic(path) + ": " + message
	}
	if d.seen == nil {
		d.seen = make(map[string]struct{})
	}
	if _, duplicate := d.seen[message]; duplicate {
		return
	}
	if len(d.items) < maxDiagnostics {
		d.seen[message] = struct{}{}
		heap.Push(&d.items, message)
		return
	}
	d.omitted = true
	if message >= d.items[0] {
		return
	}
	largest := heap.Pop(&d.items).(string)
	delete(d.seen, largest)
	d.seen[message] = struct{}{}
	heap.Push(&d.items, message)
}

func (d *diagnostics) err() error {
	if len(d.items) == 0 {
		return nil
	}
	items := append([]string(nil), d.items...)
	sort.Strings(items)
	if d.omitted {
		items = append(items, "additional diagnostics omitted")
	}
	return fmt.Errorf("validation failed:\n  - %s", strings.Join(items, "\n  - "))
}

type diagnosticMaxHeap []string

func (h diagnosticMaxHeap) Len() int           { return len(h) }
func (h diagnosticMaxHeap) Less(i, j int) bool { return h[i] > h[j] }
func (h diagnosticMaxHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *diagnosticMaxHeap) Push(value any) {
	*h = append(*h, value.(string))
}

func (h *diagnosticMaxHeap) Pop() any {
	old := *h
	last := len(old) - 1
	value := old[last]
	old[last] = ""
	*h = old[:last]
	return value
}

func sanitizeDiagnostic(value string) string {
	var result strings.Builder
	result.Grow(min(len(value), maxDiagnosticLength))
	for _, r := range value {
		if result.Len() >= maxDiagnosticLength {
			result.WriteString("...")
			break
		}
		if unicode.IsControl(r) {
			escaped := fmt.Sprintf("\\u%04X", r)
			if result.Len()+len(escaped) > maxDiagnosticLength {
				result.WriteString("...")
				break
			}
			result.WriteString(escaped)
			continue
		}
		if result.Len()+utf8.RuneLen(r) > maxDiagnosticLength {
			result.WriteString("...")
			break
		}
		result.WriteRune(r)
	}
	return result.String()
}

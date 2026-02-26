package stripe

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/vango-go/vango"
)

func mustPanic(t *testing.T, fn func(), wantSubstring string) {
	t.Helper()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic")
		}
		if wantSubstring == "" {
			return
		}
		msg := fmt.Sprint(r)
		if !strings.Contains(msg, wantSubstring) {
			t.Fatalf("panic message %q does not contain %q", msg, wantSubstring)
		}
	}()

	fn()
}

func mustNotPanic(t *testing.T, fn func()) {
	t.Helper()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()

	fn()
}

func decodeDataProps(t *testing.T, node *vango.VNode) map[string]any {
	t.Helper()

	if node == nil {
		t.Fatalf("expected non-nil node")
	}
	raw, ok := node.Props["data-props"].(string)
	if !ok {
		t.Fatalf("expected string data-props, got %T", node.Props["data-props"])
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("failed to unmarshal data-props: %v", err)
	}
	return out
}

func mustHaveClass(t *testing.T, node *vango.VNode, className string) {
	t.Helper()

	raw, ok := node.Props["class"].(string)
	if !ok {
		t.Fatalf("expected string class prop, got %T", node.Props["class"])
	}
	for _, cls := range strings.Fields(raw) {
		if cls == className {
			return
		}
	}
	t.Fatalf("expected class %q in %q", className, raw)
}

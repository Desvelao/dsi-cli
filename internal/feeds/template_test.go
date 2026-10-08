package feeds

import (
	"testing"

	"github.com/Desvelao/dsi-cli/internal/strutil"
)

func tmplVars(kv ...string) *strutil.OrderedMap {
	m := strutil.NewOrderedMap()
	for i := 0; i+1 < len(kv); i += 2 {
		m.Set(kv[i], kv[i+1])
	}
	return m
}

func TestReplaceTemplateVariablesNoReexpansion(t *testing.T) {
	for _, vars := range []*strutil.OrderedMap{
		tmplVars("a", "{{ b }}", "b", "X"),
		tmplVars("b", "X", "a", "{{ b }}"),
	} {
		got := ReplaceTemplateVariables("{{ a }}|{{ b }}", nil, vars)
		if got != "{{ b }}|X" {
			t.Fatalf("got %q", got)
		}
	}
}

func TestReplaceTemplateVariablesUnknownUntouched(t *testing.T) {
	got := ReplaceTemplateVariables("{{ nope }} {{nope}} {{ a }}", tmplVars("x", "1"), tmplVars("a", "2"))
	if got != "{{ nope }} {{nope}} 2" {
		t.Fatalf("got %q", got)
	}
}

func TestReplaceTemplateVariablesVarsOverrideMetadata(t *testing.T) {
	got := ReplaceTemplateVariables("{{ k }}", tmplVars("k", "meta"), tmplVars("k", "var"))
	if got != "var" {
		t.Fatalf("got %q", got)
	}
}

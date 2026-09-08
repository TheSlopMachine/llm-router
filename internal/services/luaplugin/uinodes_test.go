package luaplugin

import (
	"strings"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func schemaNodes(t *testing.T, body string) ([]*models.UINode, error) {
	t.Helper()
	svc, err := New(testutil.SetupTestDB(t), nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	src := `--- @plugin Schema Probe
--- @author tester
--- @version 1.0.0
--- @router_version 0.0.4
--- @allow_host example.com

llm_router.register("probe", {
  complete = function() end,
  credential_schema = function()
    return { ` + body + ` }
  end,
})
`
	if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install probe plugin: %v", err)
	}
	return svc.Schema("probe", "credential_schema")
}

func TestUINodeNewKindsValid(t *testing.T) {
	nodes, err := schemaNodes(t, `
    { type = "flow", direction = "horizontal", gap = "lg", align = "center", justify = "between", wrap = false, content = {
      { type = "input", name = "a", label = "A" },
      { type = "button", text = "Go", form_action = "submit" },
    } },
    { type = "grid", columns = 2, content = {
      { type = "input", name = "b", label = "B" },
      { type = "input", name = "c", label = "C" },
    } },
    { type = "section", title = "T", subtitle = "S", content = {
      { type = "text", text = "hi" },
    } },
    { type = "spacer" },
    { type = "spacer", size = "lg", grow = false },
    { type = "divider" },
    { type = "secret", name = "token", label = "Token", required = true },
    { type = "code", text = "ABCD-1234", label = "Code" },
  `)
	if err != nil {
		t.Fatalf("valid tree rejected: %v", err)
	}
	if len(nodes) != 8 {
		t.Fatalf("expected 8 nodes, got %d", len(nodes))
	}
	flow := nodes[0]
	if flow.Direction != "horizontal" || flow.Gap != "lg" || flow.Align != "center" || flow.Justify != "between" || flow.Wrap {
		t.Errorf("flow attrs wrong: %+v", flow)
	}
	if nodes[1].Columns != 2 {
		t.Errorf("grid columns wrong: %+v", nodes[1])
	}
	if nodes[3].Grow != true {
		t.Errorf("bare spacer must default to grow: %+v", nodes[3])
	}
}

func TestUINodeDefaultsApplied(t *testing.T) {
	nodes, err := schemaNodes(t, `
    { type = "flow", content = { { type = "text", text = "x" } } },
  `)
	if err != nil {
		t.Fatalf("valid tree rejected: %v", err)
	}
	flow := nodes[0]
	if flow.Direction != "vertical" || flow.Gap != "md" || flow.Align != "stretch" || flow.Justify != "start" || !flow.Wrap {
		t.Errorf("flow defaults wrong: %+v", flow)
	}
}

func TestUINodeNewKindsInvalid(t *testing.T) {
	cases := map[string]string{
		"unknown type":         `{ type = "widget" }`,
		"flow bad direction":   `{ type = "flow", direction = "diagonal", content = { { type = "text", text = "x" } } }`,
		"flow bad gap":         `{ type = "flow", gap = "xl", content = { { type = "text", text = "x" } } }`,
		"flow bad align":       `{ type = "flow", align = "middle", content = { { type = "text", text = "x" } } }`,
		"flow bad justify":     `{ type = "flow", justify = "around", content = { { type = "text", text = "x" } } }`,
		"flow empty content":   `{ type = "flow", content = {} }`,
		"flow wrap non-bool":   `{ type = "flow", wrap = "yes", content = { { type = "text", text = "x" } } }`,
		"grid columns zero":    `{ type = "grid", columns = 0, content = { { type = "text", text = "x" } } }`,
		"grid columns seven":   `{ type = "grid", columns = 7, content = { { type = "text", text = "x" } } }`,
		"grid columns string":  `{ type = "grid", columns = "two", content = { { type = "text", text = "x" } } }`,
		"grid empty content":   `{ type = "grid", columns = 2, content = {} }`,
		"section no title":     `{ type = "section", content = { { type = "text", text = "x" } } }`,
		"section title long":   `{ type = "section", title = "` + strings.Repeat("t", 121) + `", content = { { type = "text", text = "x" } } }`,
		"spacer with content":  `{ type = "spacer", content = { { type = "text", text = "x" } } }`,
		"spacer bad size":      `{ type = "spacer", size = "xl" }`,
		"spacer grow non-bool": `{ type = "spacer", grow = "yes" }`,
		"divider with content": `{ type = "divider", content = { { type = "text", text = "x" } } }`,
		"secret without name":  `{ type = "secret", label = "Token" }`,
		"code without text":    `{ type = "code", label = "Code" }`,
	}
	for name, body := range cases {
		if _, err := schemaNodes(t, body); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

package luaplugin

import (
	"encoding/json"
	"strconv"
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
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("probe", {
  complete = function() end,
  credential_schema = { ` + body + ` },
})
`
	if _, err := svc.Install([]byte(src), PluginOrigin{Manual: true}); err != nil {
		return nil, err
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
	if flow.Direction != "horizontal" || flow.Gap != 6 || flow.Align != "center" || flow.Justify != "between" || flow.Wrap {
		t.Errorf("flow attrs wrong: %+v", flow)
	}
	if nodes[1].Columns != 2 {
		t.Errorf("grid columns wrong: %+v", nodes[1])
	}
	if nodes[1].Gap != 4 {
		t.Errorf("grid default gap wrong: %+v", nodes[1])
	}
	if nodes[3].Grow != true {
		t.Errorf("bare spacer must default to grow: %+v", nodes[3])
	}
	if nodes[4].Size != 6 {
		t.Errorf("spacer size wrong: %+v", nodes[4])
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
	if flow.Direction != "vertical" || flow.Gap != 4 || flow.Align != "stretch" || flow.Justify != "start" || !flow.Wrap {
		t.Errorf("flow defaults wrong: %+v", flow)
	}
}

func TestUINodeNewKindsInvalid(t *testing.T) {
	cases := map[string]string{
		"unknown type":         `{ type = "widget" }`,
		"flow bad direction":   `{ type = "flow", direction = "diagonal", content = { { type = "text", text = "x" } } }`,
		"flow bad gap":         `{ type = "flow", gap = "xl", content = { { type = "text", text = "x" } } }`,
		"flow bad align":       `{ type = "flow", align = "middle", content = { { type = "text", text = "x" } } }`,
		"flow bad justify":     `{ type = "flow", justify = "stretch", content = { { type = "text", text = "x" } } }`,
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
		"button bad variant":   `{ type = "button", text = "Go", variant = "loud" }`,
	}
	for name, body := range cases {
		if _, err := schemaNodes(t, body); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestUINodeButtonVariantValid(t *testing.T) {
	nodes, err := schemaNodes(t, `
    { type = "button", text = "Delete", form_action = "remove", variant = "danger" },
    { type = "button", text = "Go" },
  `)
	if err != nil {
		t.Fatalf("valid tree rejected: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	if nodes[0].Variant != "danger" {
		t.Errorf("button variant wrong: %+v", nodes[0])
	}
	if nodes[1].FormAction != "submit" {
		t.Errorf("button default action wrong: %+v", nodes[1])
	}
}

func TestUINodeOptionLabelsValid(t *testing.T) {
	nodes, err := schemaNodes(t, `
    { type = "select", name = "method", label = "Method",
      options = { "builder-id", "idc" },
      option_labels = { ["builder-id"] = "AWS Builder ID", ["idc"] = "IAM Identity Center" } },
  `)
	if err != nil {
		t.Fatalf("valid tree rejected: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	labels := nodes[0].OptionLabels
	if labels["builder-id"] != "AWS Builder ID" || labels["idc"] != "IAM Identity Center" {
		t.Errorf("option labels wrong: %+v", labels)
	}
}

func TestUINodeOptionLabelsInvalid(t *testing.T) {
	cases := map[string]string{
		"labels not a table": `{ type = "select", name = "m", options = { "a" }, option_labels = "x" }`,
		"labels non-string key": `{ type = "select", name = "m", options = { "a" },
      option_labels = { [1] = "One" } }`,
		"labels non-string value": `{ type = "select", name = "m", options = { "a" },
      option_labels = { ["a"] = 42 } }`,
		"labels empty key": `{ type = "select", name = "m", options = { "a" },
      option_labels = { ["  "] = "One" } }`,
		"labels unknown key": `{ type = "select", name = "m", options = { "a" },
      option_labels = { ["b"] = "Bee" } }`,
	}
	for name, body := range cases {
		if _, err := schemaNodes(t, body); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestUINodeGapLegacyEnum(t *testing.T) {
	cases := map[string]int{"sm": 2, "md": 4, "lg": 6}
	for enum, want := range cases {
		nodes, err := schemaNodes(t, `{ type = "flow", gap = "`+enum+`", content = { { type = "text", text = "x" } } }`)
		if err != nil {
			t.Fatalf("flow gap %q rejected: %v", enum, err)
		}
		if nodes[0].Gap != want {
			t.Errorf("flow gap %q: got %d, want %d", enum, nodes[0].Gap, want)
		}
		nodes, err = schemaNodes(t, `{ type = "grid", columns = 2, gap = "`+enum+`", content = { { type = "text", text = "x" } } }`)
		if err != nil {
			t.Fatalf("grid gap %q rejected: %v", enum, err)
		}
		if nodes[0].Gap != want {
			t.Errorf("grid gap %q: got %d, want %d", enum, nodes[0].Gap, want)
		}
		nodes, err = schemaNodes(t, `{ type = "spacer", size = "`+enum+`" }`)
		if err != nil {
			t.Fatalf("spacer size %q rejected: %v", enum, err)
		}
		if nodes[0].Size != want {
			t.Errorf("spacer size %q: got %d, want %d", enum, nodes[0].Size, want)
		}
	}
}

func TestUINodeGapStepInt(t *testing.T) {
	for _, want := range []int{0, 1, 2, 3, 4, 5, 6, 7, 8} {
		nodes, err := schemaNodes(t, `{ type = "flow", gap = `+strconv.Itoa(want)+`, content = { { type = "text", text = "x" } } }`)
		if err != nil {
			t.Fatalf("flow gap %d rejected: %v", want, err)
		}
		if nodes[0].Gap != want {
			t.Errorf("flow gap: got %d, want %d", nodes[0].Gap, want)
		}
	}
	nodes, err := schemaNodes(t, `{ type = "grid", columns = 2, gap = 3, content = { { type = "text", text = "x" } } }`)
	if err != nil {
		t.Fatalf("grid gap int rejected: %v", err)
	}
	if nodes[0].Gap != 3 {
		t.Errorf("grid gap: got %d, want 3", nodes[0].Gap)
	}
	nodes, err = schemaNodes(t, `{ type = "spacer", size = 0 }`)
	if err != nil {
		t.Fatalf("spacer size int rejected: %v", err)
	}
	if nodes[0].Size != 0 {
		t.Errorf("spacer size: got %d, want 0", nodes[0].Size)
	}
}

func TestUINodeGapInvalid(t *testing.T) {
	cases := map[string]string{
		"flow gap unknown enum": `{ type = "flow", gap = "xl", content = { { type = "text", text = "x" } } }`,
		"flow gap above range":  `{ type = "flow", gap = 9, content = { { type = "text", text = "x" } } }`,
		"flow gap below range":  `{ type = "flow", gap = -1, content = { { type = "text", text = "x" } } }`,
		"flow gap fractional":   `{ type = "flow", gap = 2.5, content = { { type = "text", text = "x" } } }`,
		"flow gap bool":         `{ type = "flow", gap = true, content = { { type = "text", text = "x" } } }`,
		"grid gap unknown":      `{ type = "grid", columns = 2, gap = "xl", content = { { type = "text", text = "x" } } }`,
		"grid gap above":        `{ type = "grid", columns = 2, gap = 10, content = { { type = "text", text = "x" } } }`,
		"spacer size unknown":   `{ type = "spacer", size = "xl" }`,
		"spacer size above":     `{ type = "spacer", size = 9 }`,
		"spacer size bool":      `{ type = "spacer", size = true }`,
	}
	for name, body := range cases {
		if _, err := schemaNodes(t, body); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestUINodeJustifyExtended(t *testing.T) {
	for _, justify := range []string{"start", "center", "end", "between", "around", "evenly"} {
		nodes, err := schemaNodes(t, `{ type = "flow", justify = "`+justify+`", content = { { type = "text", text = "x" } } }`)
		if err != nil {
			t.Fatalf("flow justify %q rejected: %v", justify, err)
		}
		if nodes[0].Justify != justify {
			t.Errorf("flow justify: got %q, want %q", nodes[0].Justify, justify)
		}
	}
}

func TestUINodeGapJSONIsInt(t *testing.T) {
	nodes, err := schemaNodes(t, `{ type = "flow", gap = "lg", content = { { type = "text", text = "x" } } }`)
	if err != nil {
		t.Fatalf("valid tree rejected: %v", err)
	}
	raw, err := json.Marshal(nodes[0])
	if err != nil {
		t.Fatalf("marshal node: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal node: %v", err)
	}
	gap, ok := decoded["gap"].(float64)
	if !ok || gap != 6 {
		t.Errorf("gap JSON must be int 6, got %v", decoded["gap"])
	}
}

func TestUINodeInputTypeSecret(t *testing.T) {
	nodes, err := schemaNodes(t, `{ type = "input", name = "token", label = "Token", input_type = "secret" }`)
	if err != nil {
		t.Fatalf("input_type secret rejected: %v", err)
	}
	if nodes[0].InputType != "secret" {
		t.Errorf("input_type wrong: %+v", nodes[0])
	}
	for _, inputType := range []string{"text", "password", "number"} {
		if _, err := schemaNodes(t, `{ type = "input", name = "f", input_type = "`+inputType+`" }`); err != nil {
			t.Errorf("input_type %q rejected: %v", inputType, err)
		}
	}
	if _, err := schemaNodes(t, `{ type = "input", name = "f", input_type = "email" }`); err == nil {
		t.Errorf("invalid input_type accepted")
	}
}

func TestUINodeLinkURLSchemes(t *testing.T) {
	for _, url := range []string{
		"https://example.com/docs",
		"http://example.com",
		"mailto:ops@example.com",
		"/docs/guide",
		"docs/guide",
		"HTTPS://example.com",
	} {
		if _, err := schemaNodes(t, `{ type = "link", url = "`+url+`", text = "Docs" }`); err != nil {
			t.Errorf("link url %q rejected: %v", url, err)
		}
	}
	for _, url := range []string{
		"javascript:alert(1)",
		"JaVaScRiPt:alert(1)",
		"data:text/plain,hi",
		"vbscript:msgbox(1)",
		"file:///etc/passwd",
	} {
		if _, err := schemaNodes(t, `{ type = "link", url = "`+url+`", text = "X" }`); err == nil {
			t.Errorf("link url %q accepted", url)
		}
	}
}

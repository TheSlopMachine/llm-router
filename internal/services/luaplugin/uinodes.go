package luaplugin

import (
	"fmt"
	"strings"

	"github.com/TheSlopMachine/llm-router/internal/models"
	lua "github.com/yuin/gopher-lua"
)

// parseAuthResult discriminates the three auth outcomes by table shape.
func parseAuthResult(v lua.LValue) (*models.AuthFlowResult, error) {
	tbl, ok := v.(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("auth result must be a table")
	}
	render := tbl.RawGetString("render")
	redirect := tbl.RawGetString("redirect_url")
	creds := tbl.RawGetString("credentials")
	set := 0
	out := &models.AuthFlowResult{}
	if render != lua.LNil {
		set++
		nodes, err := parseUINodes(render)
		if err != nil {
			return nil, fmt.Errorf("render: %w", err)
		}
		out.Render = nodes
	}
	if redirect != lua.LNil {
		set++
		s, ok := redirect.(lua.LString)
		if !ok || strings.TrimSpace(string(s)) == "" {
			return nil, fmt.Errorf("redirect_url must be a non-empty string")
		}
		out.RedirectURL = string(s)
	}
	if creds != lua.LNil {
		set++
		raw, err := marshalLua(creds)
		if err != nil {
			return nil, fmt.Errorf("credentials: %w", err)
		}
		m := map[string]any{}
		if err := unmarshalTo(raw, &m); err != nil {
			return nil, fmt.Errorf("credentials must be a string-keyed table: %w", err)
		}
		out.Credentials = m
	}
	if set != 1 {
		return nil, fmt.Errorf("auth result must set exactly one of render, redirect_url, credentials")
	}
	return out, nil
}

var uiNodeTypes = map[string]bool{
	"text": true, "input": true, "select": true, "checkbox": true,
	"button": true, "link": true, "banner": true, "group": true,
	"flow": true, "grid": true, "section": true, "spacer": true,
	"divider": true, "secret": true, "code": true,
}

var uiGapSizes = map[string]bool{"sm": true, "md": true, "lg": true}

// parseUINodes validates a Lua UI tree into []*models.UINode.
func parseUINodes(v lua.LValue) ([]*models.UINode, error) {
	tbl, ok := v.(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("UI tree must be an array table")
	}
	return parseUINodeList(tbl, 0)
}

func parseUINodeList(tbl *lua.LTable, depth int) ([]*models.UINode, error) {
	if depth > 8 {
		return nil, fmt.Errorf("UI tree exceeds max nesting depth")
	}
	n := tbl.Len()
	if n > 200 {
		return nil, fmt.Errorf("UI tree exceeds max node count")
	}
	out := make([]*models.UINode, 0, n)
	for i := 1; i <= n; i++ {
		item := tbl.RawGetInt(i)
		nodeTbl, ok := item.(*lua.LTable)
		if !ok {
			return nil, fmt.Errorf("node %d must be a table", i)
		}
		node, err := parseUINode(nodeTbl, depth)
		if err != nil {
			return nil, fmt.Errorf("node %d: %w", i, err)
		}
		out = append(out, node)
	}
	return out, nil
}

func parseUINode(tbl *lua.LTable, depth int) (*models.UINode, error) {
	getStr := func(key string) string {
		if v, ok := tbl.RawGetString(key).(lua.LString); ok {
			return string(v)
		}
		return ""
	}
	node := &models.UINode{
		Type:        getStr("type"),
		Text:        getStr("text"),
		Name:        getStr("name"),
		Label:       getStr("label"),
		InputType:   getStr("input_type"),
		URL:         getStr("url"),
		Variant:     getStr("variant"),
		FormAction:  getStr("form_action"),
		Placeholder: getStr("placeholder"),
		Direction:   getStr("direction"),
		Align:       getStr("align"),
		Justify:     getStr("justify"),
		Gap:         getStr("gap"),
		Title:       getStr("title"),
		Subtitle:    getStr("subtitle"),
		Size:        getStr("size"),
	}
	if v, ok := tbl.RawGetString("columns").(lua.LNumber); ok {
		node.Columns = int(v)
	} else if v := tbl.RawGetString("columns"); v != lua.LNil {
		return nil, fmt.Errorf("columns must be a number")
	}
	if b, ok := tbl.RawGetString("wrap").(lua.LBool); ok {
		node.Wrap = bool(b)
	} else if v := tbl.RawGetString("wrap"); v != lua.LNil {
		return nil, fmt.Errorf("wrap must be a boolean")
	} else if node.Type == "flow" {
		node.Wrap = true
	}
	if b, ok := tbl.RawGetString("grow").(lua.LBool); ok {
		node.Grow = bool(b)
	} else if v := tbl.RawGetString("grow"); v != lua.LNil {
		return nil, fmt.Errorf("grow must be a boolean")
	} else if node.Type == "spacer" {
		node.Grow = true
	}
	if v := tbl.RawGetString("option_labels"); v != lua.LNil {
		labelsTbl, ok := v.(*lua.LTable)
		if !ok {
			return nil, fmt.Errorf("option_labels must be a table")
		}
		labels := map[string]string{}
		var labelErr error
		labelsTbl.ForEach(func(k, val lua.LValue) {
			if labelErr != nil {
				return
			}
			ks, ok := k.(lua.LString)
			vs, ok2 := val.(lua.LString)
			if !ok || !ok2 || strings.TrimSpace(string(ks)) == "" || strings.TrimSpace(string(vs)) == "" {
				labelErr = fmt.Errorf("option_labels keys and values must be non-empty strings")
				return
			}
			labels[string(ks)] = string(vs)
		})
		if labelErr != nil {
			return nil, labelErr
		}
		node.OptionLabels = labels
	}
	if !uiNodeTypes[node.Type] {
		return nil, fmt.Errorf("unknown node type %q", node.Type)
	}
	if b, ok := tbl.RawGetString("required").(lua.LBool); ok {
		node.Required = bool(b)
	}
	if v := tbl.RawGetString("value"); v != lua.LNil {
		decoded, verr := fromLuaValue(v)
		if verr != nil {
			return nil, fmt.Errorf("value: %w", verr)
		}
		node.Value = decoded
	}
	if v := tbl.RawGetString("options"); v != lua.LNil {
		optsTbl, ok := v.(*lua.LTable)
		if !ok {
			return nil, fmt.Errorf("options must be an array table")
		}
		for i := 1; i <= optsTbl.Len(); i++ {
			s, ok := optsTbl.RawGetInt(i).(lua.LString)
			if !ok {
				return nil, fmt.Errorf("option %d must be a string", i)
			}
			node.Options = append(node.Options, string(s))
		}
	}
	if v := tbl.RawGetString("content"); v != lua.LNil {
		contentTbl, ok := v.(*lua.LTable)
		if !ok {
			return nil, fmt.Errorf("content must be an array table")
		}
		children, err := parseUINodeList(contentTbl, depth+1)
		if err != nil {
			return nil, err
		}
		node.Content = children
	}
	switch node.Type {
	case "input", "select", "checkbox":
		if strings.TrimSpace(node.Name) == "" {
			return nil, fmt.Errorf("%s node requires name", node.Type)
		}
		if node.Type == "input" && node.InputType != "" {
			switch node.InputType {
			case "text", "password", "number":
			default:
				return nil, fmt.Errorf("invalid input_type %q", node.InputType)
			}
		}
		if node.Type == "select" && len(node.OptionLabels) > 0 {
			known := map[string]bool{}
			for _, opt := range node.Options {
				known[opt] = true
			}
			for key := range node.OptionLabels {
				if !known[key] {
					return nil, fmt.Errorf("option_labels key %q is not in options", key)
				}
			}
		}
	case "link":
		if strings.TrimSpace(node.URL) == "" {
			return nil, fmt.Errorf("link node requires url")
		}
	case "button":
		if strings.TrimSpace(node.FormAction) == "" {
			node.FormAction = "submit"
		}
		if node.Variant != "" {
			switch node.Variant {
			case "primary", "secondary", "danger":
			default:
				return nil, fmt.Errorf("invalid button variant %q", node.Variant)
			}
		}
	case "banner":
		if node.Variant != "" {
			switch node.Variant {
			case "info", "error", "success":
			default:
				return nil, fmt.Errorf("invalid banner variant %q", node.Variant)
			}
		}
	case "group":
		if len(node.Content) == 0 {
			return nil, fmt.Errorf("group node requires content")
		}
	case "flow":
		if len(node.Content) == 0 {
			return nil, fmt.Errorf("flow node requires content")
		}
		if node.Direction == "" {
			node.Direction = "vertical"
		}
		switch node.Direction {
		case "horizontal", "vertical":
		default:
			return nil, fmt.Errorf("invalid flow direction %q", node.Direction)
		}
		if node.Gap == "" {
			node.Gap = "md"
		}
		if !uiGapSizes[node.Gap] {
			return nil, fmt.Errorf("invalid flow gap %q", node.Gap)
		}
		if node.Align == "" {
			node.Align = "stretch"
		}
		switch node.Align {
		case "start", "center", "end", "stretch":
		default:
			return nil, fmt.Errorf("invalid flow align %q", node.Align)
		}
		if node.Justify == "" {
			node.Justify = "start"
		}
		switch node.Justify {
		case "start", "center", "end", "between":
		default:
			return nil, fmt.Errorf("invalid flow justify %q", node.Justify)
		}
	case "grid":
		if len(node.Content) == 0 {
			return nil, fmt.Errorf("grid node requires content")
		}
		if node.Columns < 1 || node.Columns > 6 {
			return nil, fmt.Errorf("grid columns must be between 1 and 6")
		}
		if node.Gap == "" {
			node.Gap = "md"
		}
		if !uiGapSizes[node.Gap] {
			return nil, fmt.Errorf("invalid grid gap %q", node.Gap)
		}
	case "section":
		if len(node.Content) == 0 {
			return nil, fmt.Errorf("section node requires content")
		}
		if strings.TrimSpace(node.Title) == "" {
			return nil, fmt.Errorf("section node requires title")
		}
		if len(node.Title) > 120 {
			return nil, fmt.Errorf("section title exceeds 120 characters")
		}
		if len(node.Subtitle) > 240 {
			return nil, fmt.Errorf("section subtitle exceeds 240 characters")
		}
	case "spacer":
		if len(node.Content) != 0 {
			return nil, fmt.Errorf("spacer node must not have content")
		}
		if node.Size != "" && !uiGapSizes[node.Size] {
			return nil, fmt.Errorf("invalid spacer size %q", node.Size)
		}
	case "divider":
		if len(node.Content) != 0 {
			return nil, fmt.Errorf("divider node must not have content")
		}
	case "secret":
		if strings.TrimSpace(node.Name) == "" {
			return nil, fmt.Errorf("secret node requires name")
		}
	case "code":
		if strings.TrimSpace(node.Text) == "" {
			return nil, fmt.Errorf("code node requires text")
		}
	}
	return node, nil
}

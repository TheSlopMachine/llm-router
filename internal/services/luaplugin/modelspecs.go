package luaplugin

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/TheSlopMachine/llm-router/internal/models"
	lua "github.com/yuin/gopher-lua"
)

// modelSpecFields names every ModelInfo JSON field a plugin model_specs
// entry may override, derived from the struct tags so the contract follows
// the wire shape. "name" is excluded: specs clarify listed models, and the
// entry key already selects the row.
var modelSpecFields = func() map[string]bool {
	fields := map[string]bool{}
	t := reflect.TypeOf(models.ModelInfo{})
	for i := 0; i < t.NumField(); i++ {
		name := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]
		if name == "" || name == "-" || name == "name" {
			continue
		}
		fields[name] = true
	}
	return fields
}()

// parseModelSpecs validates one registration's model_specs table into
// per-model override rows keyed by model name. Unknown fields and mistyped
// values reject the plugin at install: a typo'd key must never deploy as
// a silent no-op.
func parseModelSpecs(tbl *lua.LTable, typeKey string) (map[string]models.ModelInfo, error) {
	fail := func(format string, args ...any) (map[string]models.ModelInfo, error) {
		return nil, fmt.Errorf("plugin type %q: model_specs "+format, append([]any{typeKey}, args...)...)
	}
	var names []string
	valid := true
	tbl.ForEach(func(k, _ lua.LValue) {
		s, ok := k.(lua.LString)
		if !ok || string(s) == "" {
			valid = false
			return
		}
		names = append(names, string(s))
	})
	if !valid {
		return fail("keys must be non-empty model names")
	}
	sort.Strings(names)
	out := make(map[string]models.ModelInfo, len(names))
	for _, name := range names {
		entry, ok := tbl.RawGetString(name).(*lua.LTable)
		if !ok {
			return fail("model %q must be a table", name)
		}
		var fields []string
		fieldsValid := true
		entry.ForEach(func(k, _ lua.LValue) {
			s, ok := k.(lua.LString)
			if !ok {
				fieldsValid = false
				return
			}
			fields = append(fields, string(s))
		})
		if !fieldsValid {
			return fail("model %q has non-string field keys", name)
		}
		sort.Strings(fields)
		for _, field := range fields {
			if !modelSpecFields[field] {
				return fail("model %q has unknown field %q", name, field)
			}
		}
		raw, err := marshalLua(entry)
		if err != nil {
			return fail("model %q: %v", name, err)
		}
		var info models.ModelInfo
		if err := unmarshalTo(raw, &info); err != nil {
			return fail("model %q: %v", name, err)
		}
		info.Name = name
		out[name] = info
	}
	return out, nil
}

// overlayModelInfo applies one spec row onto a discovered row. Every
// non-zero spec field wins; Name never moves (the entry key selects).
func overlayModelInfo(dst *models.ModelInfo, src models.ModelInfo) {
	if src.DisplayName != "" {
		dst.DisplayName = src.DisplayName
	}
	if src.Description != "" {
		dst.Description = src.Description
	}
	if src.RPM != 0 {
		dst.RPM = src.RPM
	}
	if src.TPM != 0 {
		dst.TPM = src.TPM
	}
	if src.RPD != 0 {
		dst.RPD = src.RPD
	}
	if src.ContextWindow != 0 {
		dst.ContextWindow = src.ContextWindow
	}
	if src.MaxTokens != 0 {
		dst.MaxTokens = src.MaxTokens
	}
	if len(src.Capabilities) > 0 {
		dst.Capabilities = src.Capabilities
	}
	if len(src.InputModalities) > 0 {
		dst.InputModalities = src.InputModalities
	}
	if len(src.OutputModalities) > 0 {
		dst.OutputModalities = src.OutputModalities
	}
	if len(src.SupportedParameters) > 0 {
		dst.SupportedParameters = src.SupportedParameters
	}
	if src.Reasoning != nil {
		dst.Reasoning = src.Reasoning
	}
	if len(src.Endpoints) > 0 {
		dst.Endpoints = src.Endpoints
	}
}

// applyModelSpecs overlays spec rows onto discovered rows by name. Specs
// for undiscovered names are ignored: specs clarify listed models, they
// never resurrect removed ones.
func applyModelSpecs(rows []models.ModelInfo, specs map[string]models.ModelInfo) []models.ModelInfo {
	if len(specs) == 0 {
		return rows
	}
	for i := range rows {
		if spec, ok := specs[rows[i].Name]; ok {
			overlayModelInfo(&rows[i], spec)
		}
	}
	return rows
}

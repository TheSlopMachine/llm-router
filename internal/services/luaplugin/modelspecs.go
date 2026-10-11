package luaplugin

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/TheSlopMachine/llm-router/internal/models"
	lua "github.com/yuin/gopher-lua"
)

// modelSpecificationFields names every ModelInfo JSON field a plugin model_specs
// entry may override, derived from the struct tags so the contract follows
// the wire shape. "name" is excluded: specifications clarify listed models, and the
// entry key already selects the row.
var modelSpecificationFields = func() map[string]bool {
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

// parseModelSpecifications validates one registration's model_specs table into
// per-model override rows keyed by model name. Unknown fields and mistyped
// values reject the plugin at install: a typo'd key must never deploy as
// a silent no-op.
func parseModelSpecifications(table *lua.LTable, typeKey string) (map[string]models.ModelInfo, error) {
	fail := func(format string, args ...any) (map[string]models.ModelInfo, error) {
		return nil, fmt.Errorf("plugin type %q: model_specs "+format, append([]any{typeKey}, args...)...)
	}
	var names []string
	valid := true
	table.ForEach(func(k, _ lua.LValue) {
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
		entry, ok := table.RawGetString(name).(*lua.LTable)
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
			if !modelSpecificationFields[field] {
				return fail("model %q has unknown field %q", name, field)
			}
		}
		raw, err := marshalLua(entry)
		if err != nil {
			return fail("model %q: %v", name, err)
		}
		var modelInformation models.ModelInfo
		if err := unmarshalTo(raw, &modelInformation); err != nil {
			return fail("model %q: %v", name, err)
		}
		modelInformation.Name = name
		out[name] = modelInformation
	}
	return out, nil
}

// overlayModelInfo applies one specification row onto a discovered row. Every
// non-zero specification field wins; Name never moves (the entry key selects).
func overlayModelInfo(target *models.ModelInfo, source models.ModelInfo) {
	if source.DisplayName != "" {
		target.DisplayName = source.DisplayName
	}
	if source.Description != "" {
		target.Description = source.Description
	}
	if source.RPM != 0 {
		target.RPM = source.RPM
	}
	if source.TPM != 0 {
		target.TPM = source.TPM
	}
	if source.RPD != 0 {
		target.RPD = source.RPD
	}
	if source.ContextWindow != 0 {
		target.ContextWindow = source.ContextWindow
	}
	if source.MaxTokens != 0 {
		target.MaxTokens = source.MaxTokens
	}
	if len(source.Capabilities) > 0 {
		target.Capabilities = source.Capabilities
	}
	if len(source.InputModalities) > 0 {
		target.InputModalities = source.InputModalities
	}
	if len(source.OutputModalities) > 0 {
		target.OutputModalities = source.OutputModalities
	}
	if len(source.SupportedParameters) > 0 {
		target.SupportedParameters = source.SupportedParameters
	}
	if source.Reasoning != nil {
		target.Reasoning = source.Reasoning
	}
	if len(source.Endpoints) > 0 {
		target.Endpoints = source.Endpoints
	}
}

// applyModelSpecifications overlays specification rows onto discovered rows by name. Specifications
// for undiscovered names are ignored: specifications clarify listed models, they
// never resurrect removed ones.
func applyModelSpecifications(rows []models.ModelInfo, specifications map[string]models.ModelInfo) []models.ModelInfo {
	if len(specifications) == 0 {
		return rows
	}
	for i := range rows {
		if specification, ok := specifications[rows[i].Name]; ok {
			overlayModelInfo(&rows[i], specification)
		}
	}
	return rows
}

package luaplugin

import (
	"context"
	"fmt"

	"github.com/TheSlopMachine/llm-router/internal/models"
	lua "github.com/yuin/gopher-lua"
)

// ModelStatus is the verdict of a check_model handler call.
type ModelStatus string

// Model verdicts: healthy keeps serving, quota_exceeded marks temporary
// exhaustion, unhealthy reports a dead model, unknown changes nothing.
const (
	ModelHealthy       ModelStatus = "healthy"
	ModelQuotaExceeded ModelStatus = "quota_exceeded"
	ModelUnhealthy     ModelStatus = "unhealthy"
	ModelUnknown       ModelStatus = "unknown"
)

// ModelHealth is the validated check_model verdict.
type ModelHealth struct {
	Status   ModelStatus
	Message  string
	Response string
}

// modelRefTable builds the check_model argument: full model id, bare
// upstream name, and open endpoint string selected by the dashboard.
func modelRefTable(L *lua.LState, model models.ModelId, endpoint string) *lua.LTable {
	table := L.NewTable()
	table.RawSetString("model", lua.LString(model.String()))
	table.RawSetString("model_name", lua.LString(model.Name()))
	table.RawSetString("endpoint", lua.LString(endpoint))
	return table
}

// CheckModel invokes the check_model handler for one model and returns its
// verdict. Missing handler reports ErrHandlerNotFound (the type never
// model-checks). Invalid verdict shapes become PluginInternalError and
// change nothing: only an explicit status maps to a probe outcome.
func (s *Service) CheckModel(goCtx context.Context, metadata HandlerMeta, endpoint string) (ModelHealth, error) {
	record, err := s.Lookup(metadata.TypeKey)
	if err != nil {
		s.logLookupFailed(metadata.TypeKey, string(HandlerCheckModel), err)
		return ModelHealth{}, err
	}
	var health ModelHealth
	found, _, err := s.handlerCallEx(goCtx, record, metadata, string(HandlerCheckModel), func(L *lua.LState) {
		L.Push(ctxTable(L, "", metadata.ProviderConfig))
		L.Push(modelRefTable(L, metadata.Model, endpoint))
	}, 1, func(L *lua.LState) error {
		table, ok := L.Get(-1).(*lua.LTable)
		if !ok {
			s.recordCrash(record.ID, metadata.TypeKey, "check_model must return a table")
			return &models.PluginInternalError{PluginID: record.ID, TypeKey: metadata.TypeKey, Cause: "check_model must return a table"}
		}
		raw, ok := table.RawGetString("status").(lua.LString)
		if !ok {
			s.recordCrash(record.ID, metadata.TypeKey, "check_model table needs a status string")
			return &models.PluginInternalError{PluginID: record.ID, TypeKey: metadata.TypeKey, Cause: "check_model table needs a status string"}
		}
		switch ModelStatus(raw) {
		case ModelHealthy, ModelQuotaExceeded, ModelUnhealthy, ModelUnknown:
			health.Status = ModelStatus(raw)
		default:
			s.recordCrash(record.ID, metadata.TypeKey, "check_model has unknown status")
			return &models.PluginInternalError{PluginID: record.ID, TypeKey: metadata.TypeKey, Cause: fmt.Sprintf("check_model has unknown status %q", string(raw))}
		}
		if value := table.RawGetString("message"); value != lua.LNil {
			text, ok := value.(lua.LString)
			if !ok {
				s.recordCrash(record.ID, metadata.TypeKey, "check_model message must be a string")
				return &models.PluginInternalError{PluginID: record.ID, TypeKey: metadata.TypeKey, Cause: "check_model message must be a string"}
			}
			health.Message = string(text)
		}
		if value := table.RawGetString("response"); value != lua.LNil {
			text, ok := value.(lua.LString)
			if !ok {
				s.recordCrash(record.ID, metadata.TypeKey, "check_model response must be a string")
				return &models.PluginInternalError{PluginID: record.ID, TypeKey: metadata.TypeKey, Cause: "check_model response must be a string"}
			}
			health.Response = string(text)
		}
		return nil
	}, nil)
	if err != nil {
		return ModelHealth{}, err
	}
	if !found {
		return ModelHealth{}, &notFoundError{PluginID: record.ID, TypeKey: metadata.TypeKey, Handler: string(HandlerCheckModel)}
	}
	s.logModelVerdict(record.ID, metadata, endpoint, health)
	return health, nil
}

// logModelVerdict records a valid model verdict at debug level with model
// context. Variable detail travels in reason, never in text.
func (s *Service) logModelVerdict(pluginID string, metadata HandlerMeta, endpoint string, health ModelHealth) {
	if s.logger == nil {
		return
	}
	arguments := []any{"plugin_id", pluginID, "type", metadata.TypeKey, "model", metadata.Model.String(), "endpoint", endpoint, "status", string(health.Status)}
	if health.Message != "" {
		arguments = append(arguments, "reason", health.Message)
	}
	s.logger.Debug("Model health verdict", arguments...)
}

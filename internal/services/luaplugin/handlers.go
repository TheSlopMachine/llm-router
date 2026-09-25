package luaplugin

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	lua "github.com/yuin/gopher-lua"
)

// ctxTable builds the ctx argument for handlers. Auth flows carry flow_id;
// request handlers additionally expose provider_config when non-empty.
func ctxTable(L *lua.LState, flowID string, providerConfig map[string]any) *lua.LTable {
	tbl := L.NewTable()
	if flowID != "" {
		tbl.RawSetString("flow_id", lua.LString(flowID))
	}
	if len(providerConfig) > 0 {
		tbl.RawSetString("provider_config", toLuaValue(L, providerConfig))
	}
	return tbl
}

func credTable(L *lua.LState, cred *models.Credential) *lua.LTable {
	tbl := L.NewTable()
	if cred == nil {
		tbl.RawSetString("id", lua.LString(""))
		tbl.RawSetString("data", L.NewTable())
		return tbl
	}
	tbl.RawSetString("id", lua.LString(cred.ID))
	if cred.Data == nil {
		tbl.RawSetString("data", L.NewTable())
	} else {
		tbl.RawSetString("data", toLuaValue(L, cred.Data))
	}
	return tbl
}

func requestTable(L *lua.LState, req *models.ChatCompletionRequest) *lua.LTable {
	v := goToLuaJSON(L, req)
	tbl, ok := v.(*lua.LTable)
	if !ok {
		tbl = L.NewTable()
	}
	// The stream flag is absent by contract: stream/non-stream is
	// determined by which handler is invoked.
	tbl.RawSetString("stream", lua.LNil)
	// model_name is the bare model without the provider prefix, for
	// upstream payloads. model keeps the full ModelId.
	tbl.RawSetString("model_name", lua.LString(req.Model.Name()))
	return tbl
}

// Complete invokes the complete handler and validates the response shape.
// Proxy retries belong to the higher-level retry engine: one handler call
// uses one ordered pick list, and transport failover stays inside it.
func (s *Service) Complete(
	goCtx context.Context,
	meta HandlerMeta,
	req *models.ChatCompletionRequest,
) (*models.ChatCompletionResponse, error) {
	return callAndDecode(s, goCtx, meta, "complete", func(L *lua.LState) {
		L.Push(ctxTable(L, "", meta.ProviderConfig))
		L.Push(credTable(L, meta.Credential))
		L.Push(requestTable(L, req))
	}, []string{"choices", "tool_calls"}, func(out *models.ChatCompletionResponse) error {
		if len(out.Choices) == 0 {
			return errEmptyChoices
		}
		return nil
	})
}

// CompleteStream invokes complete_stream with an emit callback. When the
// plugin does not declare complete_stream, the stream is emulated with a
// single chunk produced by complete plus [DONE].
func (s *Service) CompleteStream(
	goCtx context.Context,
	meta HandlerMeta,
	req *models.ChatCompletionRequest,
	w io.Writer,
) error {
	rec, err := s.Lookup(meta.TypeKey)
	if err != nil {
		return err
	}
	emitFn := func(L *lua.LState) int {
		chunkVal := L.Get(1)
		raw, merr := marshalLua(chunkVal)
		if merr != nil {
			L.RaiseError("emit: encode chunk: %s", merr.Error())
			return 0
		}
		var chunk models.StreamChunk
		if uerr := unmarshalTo(normalizeEmptyObjects(raw, "choices", "tool_calls"), &chunk); uerr != nil {
			L.RaiseError("emit: invalid chunk shape: %s", uerr.Error())
			return 0
		}
		if len(chunk.Choices) == 0 && chunk.Usage == nil {
			L.RaiseError("emit: chunk has neither choices nor usage")
			return 0
		}
		// Emit the canonical struct encoding, not the raw plugin bytes:
		// this normalizes alias fields (reasoning -> reasoning_content)
		// and keeps the wire shape identical to non-stream responses.
		canonical, cerr := marshalGoJSON(chunk)
		if cerr != nil {
			L.RaiseError("emit: encode canonical chunk: %s", cerr.Error())
			return 0
		}
		if _, werr := fmt.Fprintf(w, "data: %s\n\n", string(canonical)); werr != nil {
			L.RaiseError("emit: write: %s", werr.Error())
			return 0
		}
		return 0
	}
	found, _, callErr := s.handlerCallRouted(goCtx, rec, meta, "complete_stream", func(L *lua.LState) {
		L.Push(ctxTable(L, "", meta.ProviderConfig))
		L.Push(credTable(L, meta.Credential))
		L.Push(requestTable(L, req))
		L.Push(L.NewFunction(emitFn))
	}, 2, func(L *lua.LState) error {
		_, rawErr := splitReturn(L)
		return s.contractErrOrInternal(rec, meta.TypeKey, rawErr)
	})
	if callErr != nil {
		return callErr
	}
	if found {
		if _, werr := io.WriteString(w, "data: [DONE]\n\n"); werr != nil {
			return werr
		}
		return nil
	}
	// Fallback: emulate streaming over complete().
	resp, cerr := s.Complete(goCtx, meta, req)
	if cerr != nil {
		return cerr
	}
	chunk := models.StreamChunk{
		ID: resp.ID, Object: "chat.completion.chunk",
		Created: time.Now().Unix(), Model: resp.Model,
	}
	if len(resp.Choices) > 0 {
		msg := resp.Choices[0].Message
		finish := resp.Choices[0].FinishReason
		chunk.Choices = []models.StreamChunkChoice{{
			Index: 0, Delta: models.ChatMessage{Role: msg.Role, Content: msg.Content, ToolCalls: msg.ToolCalls},
			FinishReason: &finish,
		}}
	}
	raw, _ := marshalLuaChunk(chunk)
	if _, werr := fmt.Fprintf(w, "data: %s\n\n", string(raw)); werr != nil {
		return werr
	}
	_, werr := io.WriteString(w, "data: [DONE]\n\n")
	return werr
}

func marshalLuaChunk(chunk models.StreamChunk) ([]byte, error) {
	raw, err := marshalGoJSON(chunk)
	return raw, err
}

// transcriptionRequestTable builds the transcribe request table. The audio
// bytes pass as a raw Lua string — a JSON round trip would base64 them.
// needs_segments tells the plugin to fetch segment timestamps (the router
// renders srt/vtt from segments).
func transcriptionRequestTable(L *lua.LState, req *models.TranscriptionRequest) *lua.LTable {
	tbl := L.NewTable()
	tbl.RawSetString("model", lua.LString(req.Model.String()))
	tbl.RawSetString("model_name", lua.LString(req.Model.Name()))
	tbl.RawSetString("file", lua.LString(string(req.File)))
	tbl.RawSetString("file_name", lua.LString(req.FileName))
	tbl.RawSetString("content_type", lua.LString(req.ContentType))
	if req.Language != "" {
		tbl.RawSetString("language", lua.LString(req.Language))
	}
	if req.Prompt != "" {
		tbl.RawSetString("prompt", lua.LString(req.Prompt))
	}
	if req.ResponseFormat != "" {
		tbl.RawSetString("response_format", lua.LString(req.ResponseFormat))
	}
	if req.Temperature != nil {
		tbl.RawSetString("temperature", lua.LNumber(*req.Temperature))
	}
	if len(req.TimestampGranularities) > 0 {
		gran := L.NewTable()
		for _, g := range req.TimestampGranularities {
			gran.Append(lua.LString(g))
		}
		tbl.RawSetString("timestamp_granularities", gran)
	}
	tbl.RawSetString("needs_segments", lua.LBool(req.ResponseFormat == "srt" || req.ResponseFormat == "vtt"))
	return tbl
}

// Transcribe invokes the transcribe handler. A missing handler reports
// ErrHandlerNotFound so the router maps it to a clean "endpoint not
// supported" error.
func (s *Service) Transcribe(
	goCtx context.Context,
	meta HandlerMeta,
	req *models.TranscriptionRequest,
) (*models.TranscriptionResponse, error) {
	return callAndDecode[models.TranscriptionResponse](s, goCtx, meta, "transcribe", func(L *lua.LState) {
		L.Push(ctxTable(L, "", meta.ProviderConfig))
		L.Push(credTable(L, meta.Credential))
		L.Push(transcriptionRequestTable(L, req))
	}, []string{"segments", "words"}, nil)
}

// speechRequestTable builds the speech request table.
func speechRequestTable(L *lua.LState, req *models.SpeechRequest) *lua.LTable {
	tbl := L.NewTable()
	tbl.RawSetString("model", lua.LString(req.Model.String()))
	tbl.RawSetString("model_name", lua.LString(req.Model.Name()))
	tbl.RawSetString("input", lua.LString(req.Input))
	if req.Voice != "" {
		tbl.RawSetString("voice", lua.LString(req.Voice))
	}
	if req.ResponseFormat != "" {
		tbl.RawSetString("response_format", lua.LString(req.ResponseFormat))
	}
	if req.Speed != nil {
		tbl.RawSetString("speed", lua.LNumber(*req.Speed))
	}
	if req.Instructions != "" {
		tbl.RawSetString("instructions", lua.LString(req.Instructions))
	}
	return tbl
}

// Speech invokes the speech handler. The plugin returns {audio_b64,
// format}: the JSON return path cannot carry raw bytes, so audio crosses
// the boundary base64-encoded. A missing handler reports ErrHandlerNotFound
// so the router maps it to a clean "endpoint not supported" error.
func (s *Service) Speech(
	goCtx context.Context,
	meta HandlerMeta,
	req *models.SpeechRequest,
) (*models.SpeechResponse, error) {
	out, err := callAndDecode(s, goCtx, meta, "speech", func(L *lua.LState) {
		L.Push(ctxTable(L, "", meta.ProviderConfig))
		L.Push(credTable(L, meta.Credential))
		L.Push(speechRequestTable(L, req))
	}, nil, func(payload *speechPayload) error {
		audio, derr := base64.StdEncoding.DecodeString(payload.AudioB64)
		if derr != nil {
			return errInvalidAudioB64
		}
		if len(audio) == 0 {
			return errEmptyAudio
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	audio, _ := base64.StdEncoding.DecodeString(out.AudioB64)
	return &models.SpeechResponse{Audio: audio, Format: out.Format}, nil
}

// imageRequestTable builds the generate_image request table.
func imageRequestTable(L *lua.LState, req *models.ImageGenerationRequest) *lua.LTable {
	tbl := L.NewTable()
	tbl.RawSetString("model", lua.LString(req.Model.String()))
	tbl.RawSetString("model_name", lua.LString(req.Model.Name()))
	tbl.RawSetString("prompt", lua.LString(req.Prompt))
	if req.N > 0 {
		tbl.RawSetString("n", lua.LNumber(req.N))
	}
	if req.Size != "" {
		tbl.RawSetString("size", lua.LString(req.Size))
	}
	if req.Quality != "" {
		tbl.RawSetString("quality", lua.LString(req.Quality))
	}
	if req.Style != "" {
		tbl.RawSetString("style", lua.LString(req.Style))
	}
	if req.ResponseFormat != "" {
		tbl.RawSetString("response_format", lua.LString(req.ResponseFormat))
	}
	return tbl
}

// GenerateImage invokes the generate_image handler. A missing handler
// reports ErrHandlerNotFound so the router maps it to a clean "endpoint
// not supported" error.
func (s *Service) GenerateImage(
	goCtx context.Context,
	meta HandlerMeta,
	req *models.ImageGenerationRequest,
) (*models.ImageGenerationResponse, error) {
	return callAndDecode(s, goCtx, meta, "generate_image", func(L *lua.LState) {
		L.Push(ctxTable(L, "", meta.ProviderConfig))
		L.Push(credTable(L, meta.Credential))
		L.Push(imageRequestTable(L, req))
	}, []string{"data"}, func(out *models.ImageGenerationResponse) error {
		if len(out.Data) == 0 {
			return errEmptyImageData
		}
		return nil
	})
}

// embeddingsRequestTable builds the embed request table.
func embeddingsRequestTable(L *lua.LState, req *models.EmbeddingsRequest) *lua.LTable {
	tbl := L.NewTable()
	tbl.RawSetString("model", lua.LString(req.Model.String()))
	tbl.RawSetString("model_name", lua.LString(req.Model.Name()))
	input := L.NewTable()
	for _, s := range req.Input {
		input.Append(lua.LString(s))
	}
	tbl.RawSetString("input", input)
	if req.EncodingFormat != "" {
		tbl.RawSetString("encoding_format", lua.LString(req.EncodingFormat))
	}
	if req.Dimensions > 0 {
		tbl.RawSetString("dimensions", lua.LNumber(req.Dimensions))
	}
	return tbl
}

// Embed invokes the embed handler. A missing handler reports
// ErrHandlerNotFound so the router maps it to a clean "endpoint not
// supported" error.
func (s *Service) Embed(
	goCtx context.Context,
	meta HandlerMeta,
	req *models.EmbeddingsRequest,
) (*models.EmbeddingsResponse, error) {
	return callAndDecode(s, goCtx, meta, "embed", func(L *lua.LState) {
		L.Push(ctxTable(L, "", meta.ProviderConfig))
		L.Push(credTable(L, meta.Credential))
		L.Push(embeddingsRequestTable(L, req))
	}, []string{"data"}, func(out *models.EmbeddingsResponse) error {
		if len(out.Data) != len(req.Input) {
			return errEmbedLengthMismatch
		}
		for i := range out.Data {
			if len(out.Data[i].Values) == 0 {
				return errEmptyEmbeddingVector
			}
			out.Data[i].Index = i
		}
		return nil
	})
}

// ValidateCredentials calls validate_credentials. Missing handler accepts
// any data. Returns valid=false with a ProviderError on rejection.
func (s *Service) ValidateCredentials(typeKey string, data map[string]any) (bool, error) {
	rec, err := s.Lookup(typeKey)
	if err != nil {
		return false, err
	}
	if data == nil {
		data = map[string]any{}
	}
	valid := true
	found, err := s.handlerCall(context.Background(), rec, typeKey, "validate_credentials", func(L *lua.LState) {
		L.Push(toLuaValue(L, data))
	}, 2, func(L *lua.LState) error {
		result, rawErr := splitReturn(L)
		if b, ok := result.(lua.LBool); ok && !bool(b) {
			valid = false
			if rawErr == lua.LNil {
				return &models.ProviderError{StatusCode: 400, Type: models.ErrorTypeInvalidRequest, Message: "credentials rejected"}
			}
			return s.contractErrOrInternal(rec, typeKey, rawErr)
		}
		if b, ok := result.(lua.LBool); ok && bool(b) {
			return s.contractErrOrInternal(rec, typeKey, rawErr)
		}
		if result != lua.LNil {
			if _, ok := result.(lua.LBool); !ok {
				s.recordCrash(rec.ID, typeKey, "validate_credentials must return boolean")
				return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "validate_credentials must return boolean"}
			}
		}
		return s.contractErrOrInternal(rec, typeKey, rawErr)
	}, nil)
	if err != nil {
		return false, err
	}
	if !found {
		return true, nil
	}
	return valid, nil
}

// GetModelInfos calls get_model_infos. Missing handler reports
// ErrHandlerNotFound so callers apply the fixed fallback.
func (s *Service) GetModelInfos(
	goCtx context.Context,
	typeKey string,
	cred *models.Credential,
	providerConfig map[string]any,
) ([]models.ModelInfo, error) {
	rec, err := s.Lookup(typeKey)
	if err != nil {
		return nil, err
	}
	var infos []models.ModelInfo
	meta := HandlerMeta{TypeKey: typeKey, Credential: cred, ProviderConfig: providerConfig}
	found, _, callErr := s.handlerCallRouted(goCtx, rec, meta, "get_model_infos", func(L *lua.LState) {
		L.Push(ctxTable(L, "", nil))
		L.Push(credTable(L, cred))
		if len(providerConfig) > 0 {
			L.Push(toLuaValue(L, providerConfig))
		} else {
			L.Push(L.NewTable())
		}
	}, 2, func(L *lua.LState) error {
		result, rawErr := splitReturn(L)
		if cerr := s.contractErrOrInternal(rec, typeKey, rawErr); cerr != nil {
			return cerr
		}
		if result == lua.LNil {
			infos = []models.ModelInfo{}
			return nil
		}
		raw, merr := marshalLua(result)
		if merr != nil {
			return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "encode model infos: " + merr.Error()}
		}
		var out []models.ModelInfo
		if uerr := unmarshalTo(raw, &out); uerr != nil {
			s.recordCrash(rec.ID, typeKey, "get_model_infos schema violation: "+uerr.Error())
			return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "get_model_infos schema violation: " + uerr.Error()}
		}
		infos = out
		return nil
	})
	if callErr != nil {
		return nil, callErr
	}
	if !found {
		return nil, &notFoundError{PluginID: rec.ID, TypeKey: typeKey, Handler: "get_model_infos"}
	}
	if infos == nil {
		infos = []models.ModelInfo{}
	}
	return infos, nil
}

// NeedsRefresh calls needs_refresh. Missing handler means not refreshable.
func (s *Service) NeedsRefresh(typeKey string, cred *models.Credential) (bool, error) {
	rec, err := s.Lookup(typeKey)
	if err != nil {
		return false, err
	}
	needs := false
	found, err := s.handlerCall(context.Background(), rec, typeKey, "needs_refresh", func(L *lua.LState) {
		L.Push(credTable(L, cred))
	}, 1, func(L *lua.LState) error {
		v := L.Get(-1)
		if b, ok := v.(lua.LBool); ok {
			needs = bool(b)
			return nil
		}
		if v == lua.LNil {
			needs = false
			return nil
		}
		s.recordCrash(rec.ID, typeKey, "needs_refresh must return boolean")
		return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "needs_refresh must return boolean"}
	}, nil)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	return needs, nil
}

// RefreshCredential calls refresh_credential and returns the new data map.
// Missing handler reports ErrHandlerNotFound (not refreshable).
func (s *Service) RefreshCredential(
	goCtx context.Context,
	typeKey string,
	cred *models.Credential,
) (map[string]any, error) {
	rec, err := s.Lookup(typeKey)
	if err != nil {
		return nil, err
	}
	var data map[string]any
	found, err := s.handlerCall(goCtx, rec, typeKey, "refresh_credential", func(L *lua.LState) {
		L.Push(ctxTable(L, "", nil))
		L.Push(credTable(L, cred))
	}, 2, func(L *lua.LState) error {
		_, rawErr := splitReturn(L)
		if cerr := s.contractErrOrInternal(rec, typeKey, rawErr); cerr != nil {
			return cerr
		}
		out, merr := luaValueToMap(L, L.GetTop()-1)
		if merr != nil {
			s.recordCrash(rec.ID, typeKey, "refresh_credential must return credential data table: "+merr.Error())
			return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "refresh_credential must return credential data table"}
		}
		data = out
		return nil
	}, nil)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, &notFoundError{PluginID: rec.ID, TypeKey: typeKey, Handler: "refresh_credential"}
	}
	return data, nil
}

// Schema calls config_schema or credential_schema. Missing handler reports
// ErrHandlerNotFound so the dashboard falls back to raw JSON input.
func (s *Service) Schema(typeKey, handler string) ([]*models.UINode, error) {
	if handler != "config_schema" && handler != "credential_schema" {
		return nil, fmt.Errorf("unknown schema handler %q", handler)
	}
	rec, err := s.Lookup(typeKey)
	if err != nil {
		return nil, err
	}
	var nodes []*models.UINode
	found, err := s.handlerCall(context.Background(), rec, typeKey, handler, nil, 1, func(L *lua.LState) error {
		v := L.Get(-1)
		if v == lua.LNil {
			nodes = nil
			return nil
		}
		parsed, verr := parseUINodes(v)
		if verr != nil {
			s.recordCrash(rec.ID, typeKey, handler+" schema violation: "+verr.Error())
			return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: handler + " schema violation: " + verr.Error()}
		}
		nodes = parsed
		return nil
	}, nil)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, &notFoundError{PluginID: rec.ID, TypeKey: typeKey, Handler: handler}
	}
	return nodes, nil
}

// AuthInitiate calls auth_initiate. Missing handler reports ErrHandlerNotFound
// so the dashboard uses the single-step credential_schema form.
func (s *Service) AuthInitiate(goCtx context.Context, typeKey, flowID string) (*models.AuthFlowResult, error) {
	rec, err := s.Lookup(typeKey)
	if err != nil {
		return nil, err
	}
	var result *models.AuthFlowResult
	found, err := s.handlerCall(goCtx, rec, typeKey, "auth_initiate", func(L *lua.LState) {
		L.Push(ctxTable(L, flowID, nil))
	}, 1, func(L *lua.LState) error {
		parsed, perr := parseAuthResult(L.Get(-1))
		if perr != nil {
			s.recordCrash(rec.ID, typeKey, "auth_initiate result violation: "+perr.Error())
			return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "auth_initiate result violation: " + perr.Error()}
		}
		result = parsed
		return nil
	}, nil)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, &notFoundError{PluginID: rec.ID, TypeKey: typeKey, Handler: "auth_initiate"}
	}
	return result, nil
}

// AuthStep calls auth_step with {action, values}.
func (s *Service) AuthStep(goCtx context.Context, typeKey, flowID, action string, values map[string]any) (*models.AuthFlowResult, error) {
	rec, err := s.Lookup(typeKey)
	if err != nil {
		return nil, err
	}
	if values == nil {
		values = map[string]any{}
	}
	var result *models.AuthFlowResult
	found, err := s.handlerCall(goCtx, rec, typeKey, "auth_step", func(L *lua.LState) {
		L.Push(ctxTable(L, flowID, nil))
		input := L.NewTable()
		input.RawSetString("action", lua.LString(action))
		input.RawSetString("values", toLuaValue(L, values))
		L.Push(input)
	}, 1, func(L *lua.LState) error {
		parsed, perr := parseAuthResult(L.Get(-1))
		if perr != nil {
			s.recordCrash(rec.ID, typeKey, "auth_step result violation: "+perr.Error())
			return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "auth_step result violation: " + perr.Error()}
		}
		result = parsed
		return nil
	}, nil)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, &notFoundError{PluginID: rec.ID, TypeKey: typeKey, Handler: "auth_step"}
	}
	return result, nil
}

// QualifiedSourceKey names one plugin's proxy-list source globally:
// <recordID>/<declared>. Declared names are unique per plugin but not
// across plugins, so the bare name cannot serve as an identity.
func QualifiedSourceKey(recordID, name string) string {
	return recordID + "/" + name
}

// ProxySourceKeys lists proxy-list sources registered by installed plugins,
// one qualified key per (record, declared name), first-seen order.
func (s *Service) ProxySourceKeys() []string {
	records, err := s.List()
	if err != nil {
		return nil
	}
	var keys []string
	seen := map[string]bool{}
	for _, rec := range records {
		for _, k := range rec.ProxySourceKeys {
			q := QualifiedSourceKey(rec.ID, k)
			if !seen[q] {
				seen[q] = true
				keys = append(keys, q)
			}
		}
	}
	return keys
}

// FetchProxies invokes the fetch_proxies handler of a proxy source plugin.
// sourceKey is qualified; the owning record is resolved exactly, and the
// routed call uses the declared name the plugin registered.
func (s *Service) FetchProxies(goCtx context.Context, sourceKey string) ([]models.ProxyCandidate, error) {
	records, err := s.List()
	if err != nil {
		return nil, err
	}
	var rec *PluginRecord
	declared := ""
	for _, r := range records {
		for _, k := range r.ProxySourceKeys {
			if QualifiedSourceKey(r.ID, k) == sourceKey {
				rec = r
				declared = k
				break
			}
		}
	}
	if rec == nil {
		return nil, fmt.Errorf("proxy source %q not found", sourceKey)
	}
	var out []models.ProxyCandidate
	found, err := s.handlerCall(goCtx, rec, declared, "fetch_proxies", nil, 1, func(L *lua.LState) error {
		result := L.Get(-1)
		if result == lua.LNil {
			out = []models.ProxyCandidate{}
			return nil
		}
		raw, merr := marshalLua(result)
		if merr != nil {
			return &models.PluginInternalError{PluginID: rec.ID, Cause: "encode proxies: " + merr.Error()}
		}
		if uerr := unmarshalTo(raw, &out); uerr != nil {
			s.recordCrash(rec.ID, sourceKey, "fetch_proxies schema violation: "+uerr.Error())
			return &models.PluginInternalError{PluginID: rec.ID, Cause: "fetch_proxies schema violation: " + uerr.Error()}
		}
		return nil
	}, nil)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, &notFoundError{PluginID: rec.ID, TypeKey: sourceKey, Handler: "fetch_proxies"}
	}
	return out, nil
}

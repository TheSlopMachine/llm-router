package luaplugin

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
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
	return tbl
}

// Complete invokes the complete handler and validates the response shape.
//
// Geo-rotation: a region-locked answer through a proxy demotes it (exec
// records the outcome) and the handler re-runs against the next pooled
// proxy — silently, until the pool is exhausted. Direct calls never loop.
func (s *Service) Complete(
	goCtx context.Context,
	typeKey string,
	cred *models.Credential,
	req *models.ChatCompletionRequest,
	providerConfig map[string]any,
) (*models.ChatCompletionResponse, error) {
	rec, err := s.Lookup(typeKey)
	if err != nil {
		return nil, err
	}
	// Geo-rotation: a region-locked answer through a proxy demotes it (exec
	// records the outcome) and the handler re-runs against the next pooled
	// proxy — silently, until the pool is exhausted. Direct calls never loop.
	tried := map[string]bool{}
	for {
		var resp *models.ChatCompletionResponse
		found, route, callErr := s.handlerCallRouted(goCtx, rec, typeKey, "complete", func(L *lua.LState) {
			L.Push(ctxTable(L, "", providerConfig))
			L.Push(credTable(L, cred))
			L.Push(requestTable(L, req))
		}, 2, func(L *lua.LState) error {
			result, rawErr := splitReturn(L)
			if cerr := s.contractErrOrInternal(rec, typeKey, rawErr); cerr != nil {
				return cerr
			}
			if result == lua.LNil {
				return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "complete returned nil result"}
			}
			raw, merr := marshalLua(result)
			if merr != nil {
				return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "encode response: " + merr.Error()}
			}
			var out models.ChatCompletionResponse
			if uerr := unmarshalTo(normalizeEmptyObjects(raw, "choices", "tool_calls"), &out); uerr != nil {
				s.recordCrash(rec.ID, typeKey, "complete schema violation: "+uerr.Error())
				return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "complete schema violation: " + uerr.Error()}
			}
			if len(out.Choices) == 0 {
				s.recordCrash(rec.ID, typeKey, "complete schema violation: empty choices")
				return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "complete schema violation: empty choices"}
			}
			resp = &out
			return nil
		}, providerConfig)
		if callErr == nil {
			if !found {
				return nil, &notFoundError{PluginID: rec.ID, TypeKey: typeKey, Handler: "complete"}
			}
			return resp, nil
		}
		next, rotErr := s.geoRotationNext(rec, providerConfig, callErr, route, tried)
		if rotErr != nil {
			return nil, rotErr
		}
		if next == "" {
			return nil, callErr
		}
	}
}

// geoRotationNext re-resolves the route after a geo rejection. It returns
// (nextID, nil) to retry, ("", nil) to surface the original error (not geo,
// or a direct route), or a resolver error. Pool exhaustion yields a single
// synthesized geo error instead of the last proxy's raw rejection.
func (s *Service) geoRotationNext(rec *PluginRecord, providerConfig map[string]any, callErr error, route string, tried map[string]bool) (string, error) {
	var perr *models.ProviderError
	if route == "" || !errors.As(callErr, &perr) || perr.Type != models.ErrorTypeGeo {
		return "", nil
	}
	tried[route] = true
	if s.proxyResolver == nil {
		return "", nil
	}
	next, _, rerr := s.proxyResolver(rec, providerConfig)
	if rerr != nil {
		return "", rerr
	}
	if next == "" || tried[next] {
		return "", &models.ProviderError{
			StatusCode: perr.StatusCode,
			Type:       models.ErrorTypeGeo,
			Message:    fmt.Sprintf("region-locked upstream: all %d pooled proxies were rejected", len(tried)),
		}
	}
	tried[next] = true
	return next, nil
}

// CompleteStream invokes complete_stream with an emit callback. When the
// plugin does not declare complete_stream, the stream is emulated with a
// single chunk produced by complete plus [DONE].
func (s *Service) CompleteStream(
	goCtx context.Context,
	typeKey string,
	cred *models.Credential,
	req *models.ChatCompletionRequest,
	w io.Writer,
	providerConfig map[string]any,
) error {
	rec, err := s.Lookup(typeKey)
	if err != nil {
		return err
	}
	tried := map[string]bool{}
	for {
		emitted := 0
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
			emitted++
			return 0
		}
		found, route, callErr := s.handlerCallRouted(goCtx, rec, typeKey, "complete_stream", func(L *lua.LState) {
			L.Push(ctxTable(L, "", providerConfig))
			L.Push(credTable(L, cred))
			L.Push(requestTable(L, req))
			L.Push(L.NewFunction(emitFn))
		}, 2, func(L *lua.LState) error {
			_, rawErr := splitReturn(L)
			return s.contractErrOrInternal(rec, typeKey, rawErr)
		}, providerConfig)
		if callErr != nil {
			// Mid-stream errors are terminal: bytes already left. Only a
			// pre-first-chunk geo rejection rotates to the next proxy.
			if emitted > 0 {
				return callErr
			}
			next, rotErr := s.geoRotationNext(rec, providerConfig, callErr, route, tried)
			if rotErr != nil {
				return rotErr
			}
			if next == "" {
				return callErr
			}
			continue
		}
		if found {
			if _, werr := io.WriteString(w, "data: [DONE]\n\n"); werr != nil {
				return werr
			}
			return nil
		}
		break
	}
	// Fallback: emulate streaming over complete().
	resp, cerr := s.Complete(goCtx, typeKey, cred, req, providerConfig)
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

// Transcribe invokes the transcribe handler with the same geo-rotation as
// Complete. A missing handler reports ErrHandlerNotFound so the router maps
// it to a clean "endpoint not supported" error.
func (s *Service) Transcribe(
	goCtx context.Context,
	typeKey string,
	cred *models.Credential,
	req *models.TranscriptionRequest,
	providerConfig map[string]any,
) (*models.TranscriptionResponse, error) {
	rec, err := s.Lookup(typeKey)
	if err != nil {
		return nil, err
	}
	tried := map[string]bool{}
	for {
		var resp *models.TranscriptionResponse
		found, route, callErr := s.handlerCallRouted(goCtx, rec, typeKey, "transcribe", func(L *lua.LState) {
			L.Push(ctxTable(L, "", providerConfig))
			L.Push(credTable(L, cred))
			L.Push(transcriptionRequestTable(L, req))
		}, 2, func(L *lua.LState) error {
			result, rawErr := splitReturn(L)
			if cerr := s.contractErrOrInternal(rec, typeKey, rawErr); cerr != nil {
				return cerr
			}
			if result == lua.LNil {
				return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "transcribe returned nil result"}
			}
			raw, merr := marshalLua(result)
			if merr != nil {
				return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "encode response: " + merr.Error()}
			}
			var out models.TranscriptionResponse
			if uerr := unmarshalTo(normalizeEmptyObjects(raw, "segments", "words"), &out); uerr != nil {
				s.recordCrash(rec.ID, typeKey, "transcribe schema violation: "+uerr.Error())
				return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "transcribe schema violation: " + uerr.Error()}
			}
			resp = &out
			return nil
		}, providerConfig)
		if callErr == nil {
			if !found {
				return nil, &notFoundError{PluginID: rec.ID, TypeKey: typeKey, Handler: "transcribe"}
			}
			return resp, nil
		}
		next, rotErr := s.geoRotationNext(rec, providerConfig, callErr, route, tried)
		if rotErr != nil {
			return nil, rotErr
		}
		if next == "" {
			return nil, callErr
		}
	}
}

// speechRequestTable builds the speech request table.
func speechRequestTable(L *lua.LState, req *models.SpeechRequest) *lua.LTable {
	tbl := L.NewTable()
	tbl.RawSetString("model", lua.LString(req.Model.String()))
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

// Speech invokes the speech handler with the same geo-rotation as Complete.
// The plugin returns {audio_b64, format}: the JSON return path cannot carry
// raw bytes, so audio crosses the boundary base64-encoded. A missing handler
// reports ErrHandlerNotFound so the router maps it to a clean
// "endpoint not supported" error.
func (s *Service) Speech(
	goCtx context.Context,
	typeKey string,
	cred *models.Credential,
	req *models.SpeechRequest,
	providerConfig map[string]any,
) (*models.SpeechResponse, error) {
	rec, err := s.Lookup(typeKey)
	if err != nil {
		return nil, err
	}
	tried := map[string]bool{}
	for {
		var resp *models.SpeechResponse
		found, route, callErr := s.handlerCallRouted(goCtx, rec, typeKey, "speech", func(L *lua.LState) {
			L.Push(ctxTable(L, "", providerConfig))
			L.Push(credTable(L, cred))
			L.Push(speechRequestTable(L, req))
		}, 2, func(L *lua.LState) error {
			result, rawErr := splitReturn(L)
			if cerr := s.contractErrOrInternal(rec, typeKey, rawErr); cerr != nil {
				return cerr
			}
			if result == lua.LNil {
				return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "speech returned nil result"}
			}
			raw, merr := marshalLua(result)
			if merr != nil {
				return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "encode response: " + merr.Error()}
			}
			var out struct {
				AudioB64 string `json:"audio_b64"`
				Format   string `json:"format"`
			}
			if uerr := unmarshalTo(raw, &out); uerr != nil {
				s.recordCrash(rec.ID, typeKey, "speech schema violation: "+uerr.Error())
				return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "speech schema violation: " + uerr.Error()}
			}
			audio, derr := base64.StdEncoding.DecodeString(out.AudioB64)
			if derr != nil {
				s.recordCrash(rec.ID, typeKey, "speech audio_b64 is not valid base64")
				return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "speech audio_b64 is not valid base64"}
			}
			if len(audio) == 0 {
				s.recordCrash(rec.ID, typeKey, "speech returned empty audio")
				return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "speech returned empty audio"}
			}
			resp = &models.SpeechResponse{Audio: audio, Format: out.Format}
			return nil
		}, providerConfig)
		if callErr == nil {
			if !found {
				return nil, &notFoundError{PluginID: rec.ID, TypeKey: typeKey, Handler: "speech"}
			}
			return resp, nil
		}
		next, rotErr := s.geoRotationNext(rec, providerConfig, callErr, route, tried)
		if rotErr != nil {
			return nil, rotErr
		}
		if next == "" {
			return nil, callErr
		}
	}
}

// imageRequestTable builds the generate_image request table.
func imageRequestTable(L *lua.LState, req *models.ImageGenerationRequest) *lua.LTable {
	tbl := L.NewTable()
	tbl.RawSetString("model", lua.LString(req.Model.String()))
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

// GenerateImage invokes the generate_image handler with the same
// geo-rotation as Complete. A missing handler reports ErrHandlerNotFound so
// the router maps it to a clean "endpoint not supported" error.
func (s *Service) GenerateImage(
	goCtx context.Context,
	typeKey string,
	cred *models.Credential,
	req *models.ImageGenerationRequest,
	providerConfig map[string]any,
) (*models.ImageGenerationResponse, error) {
	rec, err := s.Lookup(typeKey)
	if err != nil {
		return nil, err
	}
	tried := map[string]bool{}
	for {
		var resp *models.ImageGenerationResponse
		found, route, callErr := s.handlerCallRouted(goCtx, rec, typeKey, "generate_image", func(L *lua.LState) {
			L.Push(ctxTable(L, "", providerConfig))
			L.Push(credTable(L, cred))
			L.Push(imageRequestTable(L, req))
		}, 2, func(L *lua.LState) error {
			result, rawErr := splitReturn(L)
			if cerr := s.contractErrOrInternal(rec, typeKey, rawErr); cerr != nil {
				return cerr
			}
			if result == lua.LNil {
				return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "generate_image returned nil result"}
			}
			raw, merr := marshalLua(result)
			if merr != nil {
				return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "encode response: " + merr.Error()}
			}
			var out models.ImageGenerationResponse
			if uerr := unmarshalTo(normalizeEmptyObjects(raw, "data"), &out); uerr != nil {
				s.recordCrash(rec.ID, typeKey, "generate_image schema violation: "+uerr.Error())
				return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "generate_image schema violation: " + uerr.Error()}
			}
			if len(out.Data) == 0 {
				s.recordCrash(rec.ID, typeKey, "generate_image schema violation: empty data")
				return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "generate_image schema violation: empty data"}
			}
			resp = &out
			return nil
		}, providerConfig)
		if callErr == nil {
			if !found {
				return nil, &notFoundError{PluginID: rec.ID, TypeKey: typeKey, Handler: "generate_image"}
			}
			return resp, nil
		}
		next, rotErr := s.geoRotationNext(rec, providerConfig, callErr, route, tried)
		if rotErr != nil {
			return nil, rotErr
		}
		if next == "" {
			return nil, callErr
		}
	}
}

// embeddingsRequestTable builds the embed request table.
func embeddingsRequestTable(L *lua.LState, req *models.EmbeddingsRequest) *lua.LTable {
	tbl := L.NewTable()
	tbl.RawSetString("model", lua.LString(req.Model.String()))
	input := L.NewTable()
	for _, s := range req.Input {
		input.Append(lua.LString(s))
	}
	tbl.RawSetString("input", input)
	if req.Dimensions > 0 {
		tbl.RawSetString("dimensions", lua.LNumber(req.Dimensions))
	}
	return tbl
}

// Embed invokes the embed handler with the same geo-rotation as Complete.
// A missing handler reports ErrHandlerNotFound so the router maps it to a
// clean "endpoint not supported" error.
func (s *Service) Embed(
	goCtx context.Context,
	typeKey string,
	cred *models.Credential,
	req *models.EmbeddingsRequest,
	providerConfig map[string]any,
) (*models.EmbeddingsResponse, error) {
	rec, err := s.Lookup(typeKey)
	if err != nil {
		return nil, err
	}
	tried := map[string]bool{}
	for {
		var resp *models.EmbeddingsResponse
		found, route, callErr := s.handlerCallRouted(goCtx, rec, typeKey, "embed", func(L *lua.LState) {
			L.Push(ctxTable(L, "", providerConfig))
			L.Push(credTable(L, cred))
			L.Push(embeddingsRequestTable(L, req))
		}, 2, func(L *lua.LState) error {
			result, rawErr := splitReturn(L)
			if cerr := s.contractErrOrInternal(rec, typeKey, rawErr); cerr != nil {
				return cerr
			}
			if result == lua.LNil {
				return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "embed returned nil result"}
			}
			raw, merr := marshalLua(result)
			if merr != nil {
				return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "encode response: " + merr.Error()}
			}
			var out models.EmbeddingsResponse
			if uerr := unmarshalTo(normalizeEmptyObjects(raw, "data"), &out); uerr != nil {
				s.recordCrash(rec.ID, typeKey, "embed schema violation: "+uerr.Error())
				return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "embed schema violation: " + uerr.Error()}
			}
			if len(out.Data) != len(req.Input) {
				s.recordCrash(rec.ID, typeKey, "embed schema violation: data length does not match input length")
				return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "embed schema violation: data length does not match input length"}
			}
			for i := range out.Data {
				if len(out.Data[i].Values) == 0 {
					s.recordCrash(rec.ID, typeKey, "embed schema violation: empty embedding vector")
					return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "embed schema violation: empty embedding vector"}
				}
				out.Data[i].Index = i
			}
			resp = &out
			return nil
		}, providerConfig)
		if callErr == nil {
			if !found {
				return nil, &notFoundError{PluginID: rec.ID, TypeKey: typeKey, Handler: "embed"}
			}
			return resp, nil
		}
		next, rotErr := s.geoRotationNext(rec, providerConfig, callErr, route, tried)
		if rotErr != nil {
			return nil, rotErr
		}
		if next == "" {
			return nil, callErr
		}
	}
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

// GetModelInfos calls get_model_infos with the same geo-rotation as
// Complete: a region-locked answer through a proxy walks the pool instead
// of failing discovery. Missing handler reports ErrHandlerNotFound so
// callers apply the fixed fallback.
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
	tried := map[string]bool{}
	for {
		var infos []models.ModelInfo
		found, route, callErr := s.handlerCallRouted(goCtx, rec, typeKey, "get_model_infos", func(L *lua.LState) {
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
		}, providerConfig)
		if callErr == nil {
			if !found {
				return nil, &notFoundError{PluginID: rec.ID, TypeKey: typeKey, Handler: "get_model_infos"}
			}
			if infos == nil {
				infos = []models.ModelInfo{}
			}
			return infos, nil
		}
		next, rotErr := s.geoRotationNext(rec, providerConfig, callErr, route, tried)
		if rotErr != nil {
			return nil, rotErr
		}
		if next == "" {
			return nil, callErr
		}
	}
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

// ProxySourceKeys lists proxy-list sources registered by installed plugins.
func (s *Service) ProxySourceKeys() []string {
	records, err := s.List()
	if err != nil {
		return nil
	}
	var keys []string
	for _, rec := range records {
		keys = append(keys, rec.ProxySourceKeys...)
	}
	return keys
}

// FetchProxies invokes the fetch_proxies handler of a proxy source plugin.
func (s *Service) FetchProxies(goCtx context.Context, sourceKey string) ([]models.ProxyCandidate, error) {
	records, err := s.List()
	if err != nil {
		return nil, err
	}
	var rec *PluginRecord
	for _, r := range records {
		for _, k := range r.ProxySourceKeys {
			if k == sourceKey {
				rec = r
				break
			}
		}
	}
	if rec == nil {
		return nil, fmt.Errorf("proxy source %q not found", sourceKey)
	}
	var out []models.ProxyCandidate
	found, err := s.handlerCall(goCtx, rec, sourceKey, "fetch_proxies", nil, 1, func(L *lua.LState) error {
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
		node.Value = fromLuaValue(v)
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

package luaplugin

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"sync/atomic"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/proxypool"
	lua "github.com/yuin/gopher-lua"
)

// ctxTable builds the ctx argument for handlers. Auth flows carry flow_id;
// request handlers additionally expose provider_config when non-empty.
func ctxTable(L *lua.LState, flowID string, providerConfig map[string]any) *lua.LTable {
	table := L.NewTable()
	if flowID != "" {
		table.RawSetString("flow_id", lua.LString(flowID))
	}
	if len(providerConfig) > 0 {
		table.RawSetString("provider_config", toLuaValue(L, providerConfig))
	}
	return table
}

func credentialTable(L *lua.LState, credential *models.Credential) *lua.LTable {
	table := L.NewTable()
	if credential == nil {
		table.RawSetString("id", lua.LString(""))
		table.RawSetString("data", L.NewTable())
		return table
	}
	table.RawSetString("id", lua.LString(credential.ID))
	if credential.Data == nil {
		table.RawSetString("data", L.NewTable())
	} else {
		table.RawSetString("data", toLuaValue(L, credential.Data))
	}
	return table
}

func requestTable(L *lua.LState, request *models.ChatCompletionRequest) *lua.LTable {
	v := goToLuaJSON(L, request)
	table, ok := v.(*lua.LTable)
	if !ok {
		table = L.NewTable()
	}
	// The stream flag is absent by contract: stream/non-stream is
	// determined by which handler is invoked.
	table.RawSetString("stream", lua.LNil)
	// model_name is the bare model without the provider prefix, for
	// upstream payloads. model keeps the full ModelId.
	table.RawSetString("model_name", lua.LString(request.Model.Name()))
	// cache_key is the cross-turn prefix-cache partition, stable for the
	// whole conversation (see prefixCacheKey).
	table.RawSetString("cache_key", lua.LString(prefixCacheKey(request)))
	return table
}

// prefixCacheKey hashes the conversation root (model, first message,
// sorted tool names) into a stable opaque partition key. Appends keep the
// root identical, so every turn of one conversation reuses the partition
// and upstream prefix caches hit on the shared part. Different roots only
// miss, never poison: content still addresses the cache within a partition.
func prefixCacheKey(request *models.ChatCompletionRequest) string {
	if len(request.Messages) == 0 {
		return "prefix-boot"
	}
	names := make([]string, 0, len(request.Tools))
	for _, t := range request.Tools {
		name := t.Name
		if t.Function != nil && t.Function.Name != "" {
			name = t.Function.Name
		}
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	raw, err := json.Marshal(map[string]any{
		"model": request.Model.String(),
		"first": request.Messages[0],
		"tools": names,
	})
	if err != nil {
		return "prefix-boot"
	}
	sum := sha256.Sum256(raw)
	return "pck_" + hex.EncodeToString(sum[:16])
}

// Complete invokes the complete handler and validates the response shape.
// Proxy retries belong to the higher-level retry engine: one handler call
// uses one ordered pick list, and transport failover stays inside it.
func (s *Service) Complete(
	goCtx context.Context,
	handlerMetadata HandlerMeta,
	request *models.ChatCompletionRequest,
) (*models.ChatCompletionResponse, error) {
	response, _, err := s.CompleteRouted(goCtx, handlerMetadata, request)
	return response, err
}

// CompleteRouted is Complete plus the redacted proxy host:port of the call
// ("" = direct).
func (s *Service) CompleteRouted(
	goCtx context.Context,
	handlerMetadata HandlerMeta,
	request *models.ChatCompletionRequest,
) (*models.ChatCompletionResponse, string, error) {
	return callAndDecode(s, goCtx, handlerMetadata, "complete", func(L *lua.LState) {
		L.Push(ctxTable(L, "", handlerMetadata.ProviderConfig))
		L.Push(requestTable(L, request))
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
	handlerMetadata HandlerMeta,
	request *models.ChatCompletionRequest,
	w io.Writer,
) error {
	_, err := s.CompleteStreamRouted(goCtx, handlerMetadata, request, w)
	return err
}

// CompleteStreamRouted is CompleteStream plus the redacted proxy host:port
// of the call ("" = direct).
func (s *Service) CompleteStreamRouted(
	goCtx context.Context,
	handlerMetadata HandlerMeta,
	request *models.ChatCompletionRequest,
	w io.Writer,
) (string, error) {
	record, err := s.Lookup(handlerMetadata.TypeKey)
	if err != nil {
		s.logLookupFailed(handlerMetadata.TypeKey, "complete_stream", err)
		return "", err
	}
	// Tracks whether any stream output reached the client: before the first
	// write a transport failure may still be retried on another proxy.
	var delivered atomic.Bool
	handlerMetadata.delivered = &delivered
	emitFunction := func(L *lua.LState) int {
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
		delivered.Store(true)
		return 0
	}
	found, proxy, callError := s.handlerCallRouted(goCtx, record, handlerMetadata, "complete_stream", func(L *lua.LState) {
		L.Push(ctxTable(L, "", handlerMetadata.ProviderConfig))
		L.Push(requestTable(L, request))
		L.Push(L.NewFunction(emitFunction))
	}, 2, func(L *lua.LState) error {
		_, returnError := splitReturn(L)
		return s.contractErrOrInternal(record, handlerMetadata.TypeKey, returnError)
	})
	if callError != nil {
		return proxy, callError
	}
	if found {
		if _, werr := io.WriteString(w, "data: [DONE]\n\n"); werr != nil {
			return proxy, werr
		}
		return proxy, nil
	}
	// Fallback: emulate streaming over complete().
	response, fallbackProxy, cerr := s.CompleteRouted(goCtx, handlerMetadata, request)
	if cerr != nil {
		return fallbackProxy, cerr
	}
	chunk := models.StreamChunk{
		ID: response.ID, Object: "chat.completion.chunk",
		Created: time.Now().Unix(), Model: response.Model,
	}
	if len(response.Choices) > 0 {
		msg := response.Choices[0].Message
		finish := response.Choices[0].FinishReason
		chunk.Choices = []models.StreamChunkChoice{{
			Index: 0, Delta: models.ChatMessage{Role: msg.Role, Content: msg.Content, ToolCalls: msg.ToolCalls},
			FinishReason: &finish,
		}}
	}
	raw, _ := marshalLuaChunk(chunk)
	if _, werr := fmt.Fprintf(w, "data: %s\n\n", string(raw)); werr != nil {
		return fallbackProxy, werr
	}
	_, werr := io.WriteString(w, "data: [DONE]\n\n")
	return fallbackProxy, werr
}

func marshalLuaChunk(chunk models.StreamChunk) ([]byte, error) {
	raw, err := marshalGoJSON(chunk)
	return raw, err
}

// baseRequestTable builds the shared model/model_name preamble: model keeps
// the full ModelId, model_name is the bare model without the provider
// prefix, for upstream payloads. requestTable stays separate: it converts
// via goToLuaJSON plus stream=nil/cache_key.
func baseRequestTable(L *lua.LState, model models.ModelId) *lua.LTable {
	table := L.NewTable()
	table.RawSetString("model", lua.LString(model.String()))
	table.RawSetString("model_name", lua.LString(model.Name()))
	return table
}

// transcriptionRequestTable builds the transcribe request table. The audio
// bytes pass as a raw Lua string — a JSON round trip would base64 them.
// needs_segments tells the plugin to fetch segment timestamps (the router
// renders srt/vtt from segments).
func transcriptionRequestTable(L *lua.LState, request *models.TranscriptionRequest) *lua.LTable {
	table := baseRequestTable(L, request.Model)
	table.RawSetString("file", lua.LString(string(request.File)))
	table.RawSetString("file_name", lua.LString(request.FileName))
	table.RawSetString("content_type", lua.LString(request.ContentType))
	if request.Language != "" {
		table.RawSetString("language", lua.LString(request.Language))
	}
	if request.Prompt != "" {
		table.RawSetString("prompt", lua.LString(request.Prompt))
	}
	if request.ResponseFormat != "" {
		table.RawSetString("response_format", lua.LString(request.ResponseFormat))
	}
	if request.Temperature != nil {
		table.RawSetString("temperature", lua.LNumber(*request.Temperature))
	}
	if len(request.TimestampGranularities) > 0 {
		granularities := L.NewTable()
		for _, g := range request.TimestampGranularities {
			granularities.Append(lua.LString(g))
		}
		table.RawSetString("timestamp_granularities", granularities)
	}
	table.RawSetString("needs_segments", lua.LBool(request.ResponseFormat == "srt" || request.ResponseFormat == "vtt"))
	return table
}

// Transcribe invokes the transcribe handler. A missing handler reports
// ErrHandlerNotFound so the router maps it to a clean "endpoint not
// supported" error.
func (s *Service) Transcribe(
	goCtx context.Context,
	handlerMetadata HandlerMeta,
	request *models.TranscriptionRequest,
) (*models.TranscriptionResponse, error) {
	response, _, err := s.TranscribeRouted(goCtx, handlerMetadata, request)
	return response, err
}

// TranscribeRouted is Transcribe plus the redacted proxy host:port of the
// call ("" = direct).
func (s *Service) TranscribeRouted(
	goCtx context.Context,
	handlerMetadata HandlerMeta,
	request *models.TranscriptionRequest,
) (*models.TranscriptionResponse, string, error) {
	return callAndDecode[models.TranscriptionResponse](s, goCtx, handlerMetadata, "transcribe", func(L *lua.LState) {
		L.Push(ctxTable(L, "", handlerMetadata.ProviderConfig))
		L.Push(transcriptionRequestTable(L, request))
	}, []string{"segments", "words"}, nil)
}

// speechRequestTable builds the speech request table.
func speechRequestTable(L *lua.LState, request *models.SpeechRequest) *lua.LTable {
	table := baseRequestTable(L, request.Model)
	table.RawSetString("input", lua.LString(request.Input))
	if request.Voice != "" {
		table.RawSetString("voice", lua.LString(request.Voice))
	}
	if request.ResponseFormat != "" {
		table.RawSetString("response_format", lua.LString(request.ResponseFormat))
	}
	if request.Speed != nil {
		table.RawSetString("speed", lua.LNumber(*request.Speed))
	}
	if request.Instructions != "" {
		table.RawSetString("instructions", lua.LString(request.Instructions))
	}
	return table
}

// Speech invokes the speech handler. The plugin returns {audio_b64,
// format}: the JSON return path cannot carry raw bytes, so audio crosses
// the boundary base64-encoded. A missing handler reports ErrHandlerNotFound
// so the router maps it to a clean "endpoint not supported" error.
func (s *Service) Speech(
	goCtx context.Context,
	handlerMetadata HandlerMeta,
	request *models.SpeechRequest,
) (*models.SpeechResponse, error) {
	response, _, err := s.SpeechRouted(goCtx, handlerMetadata, request)
	return response, err
}

// SpeechRouted is Speech plus the redacted proxy host:port of the call
// ("" = direct).
func (s *Service) SpeechRouted(
	goCtx context.Context,
	handlerMetadata HandlerMeta,
	request *models.SpeechRequest,
) (*models.SpeechResponse, string, error) {
	out, proxy, err := callAndDecode(s, goCtx, handlerMetadata, "speech", func(L *lua.LState) {
		L.Push(ctxTable(L, "", handlerMetadata.ProviderConfig))
		L.Push(speechRequestTable(L, request))
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
		return nil, proxy, err
	}
	audio, _ := base64.StdEncoding.DecodeString(out.AudioB64)
	return &models.SpeechResponse{Audio: audio, Format: out.Format}, proxy, nil
}

// imageRequestTable builds the generate_image request table.
func imageRequestTable(L *lua.LState, request *models.ImageGenerationRequest) *lua.LTable {
	table := baseRequestTable(L, request.Model)
	table.RawSetString("prompt", lua.LString(request.Prompt))
	if request.N > 0 {
		table.RawSetString("n", lua.LNumber(request.N))
	}
	if request.Size != "" {
		table.RawSetString("size", lua.LString(request.Size))
	}
	if request.Quality != "" {
		table.RawSetString("quality", lua.LString(request.Quality))
	}
	if request.Style != "" {
		table.RawSetString("style", lua.LString(request.Style))
	}
	if request.ResponseFormat != "" {
		table.RawSetString("response_format", lua.LString(request.ResponseFormat))
	}
	if request.ImageB64 != "" {
		table.RawSetString("image_b64", lua.LString(request.ImageB64))
	}
	if request.ImageName != "" {
		table.RawSetString("image_name", lua.LString(request.ImageName))
	}
	if request.MaskB64 != "" {
		table.RawSetString("mask_b64", lua.LString(request.MaskB64))
	}
	return table
}

// GenerateImage invokes the generate_image handler. A missing handler
// reports ErrHandlerNotFound so the router maps it to a clean "endpoint
// not supported" error.
func (s *Service) GenerateImage(
	goCtx context.Context,
	handlerMetadata HandlerMeta,
	request *models.ImageGenerationRequest,
) (*models.ImageGenerationResponse, error) {
	response, _, err := s.GenerateImageRouted(goCtx, handlerMetadata, request)
	return response, err
}

// GenerateImageRouted is GenerateImage plus the redacted proxy host:port of
// the call ("" = direct).
func (s *Service) GenerateImageRouted(
	goCtx context.Context,
	handlerMetadata HandlerMeta,
	request *models.ImageGenerationRequest,
) (*models.ImageGenerationResponse, string, error) {
	return callAndDecode(s, goCtx, handlerMetadata, "generate_image", func(L *lua.LState) {
		L.Push(ctxTable(L, "", handlerMetadata.ProviderConfig))
		L.Push(imageRequestTable(L, request))
	}, []string{"data"}, func(out *models.ImageGenerationResponse) error {
		if len(out.Data) == 0 {
			return errEmptyImageData
		}
		return nil
	})
}

// embeddingsRequestTable builds the embed request table.
func embeddingsRequestTable(L *lua.LState, request *models.EmbeddingsRequest) *lua.LTable {
	table := baseRequestTable(L, request.Model)
	input := L.NewTable()
	for _, s := range request.Input {
		input.Append(lua.LString(s))
	}
	table.RawSetString("input", input)
	if request.EncodingFormat != "" {
		table.RawSetString("encoding_format", lua.LString(request.EncodingFormat))
	}
	if request.Dimensions > 0 {
		table.RawSetString("dimensions", lua.LNumber(request.Dimensions))
	}
	return table
}

// Embed invokes the embed handler. A missing handler reports
// ErrHandlerNotFound so the router maps it to a clean "endpoint not
// supported" error.
func (s *Service) Embed(
	goCtx context.Context,
	handlerMetadata HandlerMeta,
	request *models.EmbeddingsRequest,
) (*models.EmbeddingsResponse, error) {
	response, _, err := s.EmbedRouted(goCtx, handlerMetadata, request)
	return response, err
}

// EmbedRouted is Embed plus the redacted proxy host:port of the call
// ("" = direct).
func (s *Service) EmbedRouted(
	goCtx context.Context,
	handlerMetadata HandlerMeta,
	request *models.EmbeddingsRequest,
) (*models.EmbeddingsResponse, string, error) {
	return callAndDecode(s, goCtx, handlerMetadata, "embed", func(L *lua.LState) {
		L.Push(ctxTable(L, "", handlerMetadata.ProviderConfig))
		L.Push(embeddingsRequestTable(L, request))
	}, []string{"data"}, func(out *models.EmbeddingsResponse) error {
		if len(out.Data) != len(request.Input) {
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

// Moderate invokes the moderate handler. A missing handler reports
// ErrHandlerNotFound so the router maps it to a clean "endpoint not
// supported" error.
func (s *Service) Moderate(
	goCtx context.Context,
	handlerMetadata HandlerMeta,
	request *models.ModerationRequest,
) (*models.ModerationResponse, error) {
	response, _, err := s.ModerateRouted(goCtx, handlerMetadata, request)
	return response, err
}

// ModerateRouted is Moderate plus the redacted proxy host:port of the call
// ("" = direct).
func (s *Service) ModerateRouted(
	goCtx context.Context,
	handlerMetadata HandlerMeta,
	request *models.ModerationRequest,
) (*models.ModerationResponse, string, error) {
	return callAndDecode(s, goCtx, handlerMetadata, "moderate", func(L *lua.LState) {
		L.Push(ctxTable(L, "", handlerMetadata.ProviderConfig))
		L.Push(moderateRequestTable(L, request))
	}, []string{"results"}, func(out *models.ModerationResponse) error {
		if len(out.Results) != len(request.Input) {
			return errModerationLengthMismatch
		}
		if out.ID == "" {
			out.ID = "modr_router"
		}
		if out.Model == "" {
			out.Model = request.Model.String()
		}
		for i := range out.Results {
			if out.Results[i].Categories == nil {
				out.Results[i].Categories = map[string]bool{}
			}
			if out.Results[i].CategoryScores == nil {
				out.Results[i].CategoryScores = map[string]float64{}
			}
		}
		return nil
	})
}

// moderateRequestTable builds the moderate request table.
func moderateRequestTable(L *lua.LState, request *models.ModerationRequest) *lua.LTable {
	table := baseRequestTable(L, request.Model)
	input := L.NewTable()
	for _, s := range request.Input {
		input.Append(lua.LString(s))
	}
	table.RawSetString("input", input)
	return table
}

// videoSubmitTable builds the generate_video request table. Optional fields
// enter only when set; callback_url travels for storage only and upstreams
// must ignore unknown properties the same way they ignore model_name.
func videoSubmitTable(L *lua.LState, request *models.VideoGenerationRequest) *lua.LTable {
	table := baseRequestTable(L, request.Model)
	if request.Prompt != "" {
		table.RawSetString("prompt", lua.LString(request.Prompt))
	}
	if request.Duration > 0 {
		table.RawSetString("duration", lua.LNumber(request.Duration))
	}
	if request.Resolution != "" {
		table.RawSetString("resolution", lua.LString(request.Resolution))
	}
	if request.AspectRatio != "" {
		table.RawSetString("aspect_ratio", lua.LString(request.AspectRatio))
	}
	if request.Size != "" {
		table.RawSetString("size", lua.LString(request.Size))
	}
	if request.Seed != nil {
		table.RawSetString("seed", lua.LNumber(*request.Seed))
	}
	if request.GenerateAudio != nil {
		table.RawSetString("generate_audio", lua.LBool(*request.GenerateAudio))
	}
	if len(request.FrameImages) > 0 {
		frames := L.NewTable()
		for _, f := range request.FrameImages {
			entry := L.NewTable()
			if f.Type != "" {
				entry.RawSetString("type", lua.LString(f.Type))
			}
			if f.ImageURL != nil {
				url := L.NewTable()
				url.RawSetString("url", lua.LString(f.ImageURL.URL))
				entry.RawSetString("image_url", url)
			}
			if f.FrameType != "" {
				entry.RawSetString("frame_type", lua.LString(f.FrameType))
			}
			frames.Append(entry)
		}
		table.RawSetString("frame_images", frames)
	}
	if len(request.InputReferences) > 0 {
		refs := L.NewTable()
		for _, r := range request.InputReferences {
			entry := L.NewTable()
			if r.Type != "" {
				entry.RawSetString("type", lua.LString(r.Type))
			}
			setRefURL := func(key string, u *models.VideoReferenceURL) {
				if u == nil {
					return
				}
				url := L.NewTable()
				url.RawSetString("url", lua.LString(u.URL))
				entry.RawSetString(key, url)
			}
			setRefURL("image_url", r.ImageURL)
			setRefURL("audio_url", r.AudioURL)
			setRefURL("video_url", r.VideoURL)
			refs.Append(entry)
		}
		table.RawSetString("input_references", refs)
	}
	if request.PreviousJobID != "" {
		table.RawSetString("previous_job_id", lua.LString(request.PreviousJobID))
	}
	if request.CallbackURL != "" {
		table.RawSetString("callback_url", lua.LString(request.CallbackURL))
	}
	if len(request.ProviderOptions) > 0 {
		table.RawSetString("provider", toLuaValue(L, map[string]any{"options": request.ProviderOptions}))
	}
	if request.User != "" {
		table.RawSetString("user", lua.LString(request.User))
	}
	if request.SessionID != "" {
		table.RawSetString("session_id", lua.LString(request.SessionID))
	}
	return table
}

// validateVideoResponse rejects job tables without an ID or with an
// unknown status.
func validateVideoResponse(out *models.VideoGenerationResponse) error {
	if out.ID == "" {
		return errEmptyVideoJobID
	}
	if !models.ValidVideoStatus(out.Status) {
		return errUnknownVideoStatus
	}
	return nil
}

// SubmitVideo invokes the generate_video handler. A missing handler reports
// ErrHandlerNotFound so the router maps it to a clean "endpoint not
// supported" error.
func (s *Service) SubmitVideo(
	goCtx context.Context,
	handlerMetadata HandlerMeta,
	request *models.VideoGenerationRequest,
) (*models.VideoGenerationResponse, error) {
	response, _, err := s.SubmitVideoRouted(goCtx, handlerMetadata, request)
	return response, err
}

// SubmitVideoRouted is SubmitVideo plus the redacted proxy host:port of the
// call ("" = direct).
func (s *Service) SubmitVideoRouted(
	goCtx context.Context,
	handlerMetadata HandlerMeta,
	request *models.VideoGenerationRequest,
) (*models.VideoGenerationResponse, string, error) {
	return callAndDecode(s, goCtx, handlerMetadata, "generate_video", func(L *lua.LState) {
		L.Push(ctxTable(L, "", handlerMetadata.ProviderConfig))
		L.Push(videoSubmitTable(L, request))
	}, []string{"unsigned_urls"}, validateVideoResponse)
}

// videoJobTable builds the poll_video / video_content request table: the
// upstream job ID plus the model pair for payload building.
func videoJobTable(L *lua.LState, model models.ModelId, upstreamJobID string, index int, withIndex bool) *lua.LTable {
	table := baseRequestTable(L, model)
	table.RawSetString("job_id", lua.LString(upstreamJobID))
	if withIndex {
		table.RawSetString("index", lua.LNumber(index))
	}
	return table
}

// PollVideo invokes the poll_video handler for one upstream job ID.
func (s *Service) PollVideo(
	goCtx context.Context,
	handlerMetadata HandlerMeta,
	model models.ModelId,
	upstreamJobID string,
) (*models.VideoGenerationResponse, error) {
	response, _, err := s.PollVideoRouted(goCtx, handlerMetadata, model, upstreamJobID)
	return response, err
}

// PollVideoRouted is PollVideo plus the redacted proxy host:port of the
// call ("" = direct).
func (s *Service) PollVideoRouted(
	goCtx context.Context,
	handlerMetadata HandlerMeta,
	model models.ModelId,
	upstreamJobID string,
) (*models.VideoGenerationResponse, string, error) {
	return callAndDecode(s, goCtx, handlerMetadata, "poll_video", func(L *lua.LState) {
		L.Push(ctxTable(L, "", handlerMetadata.ProviderConfig))
		L.Push(videoJobTable(L, model, upstreamJobID, 0, false))
	}, []string{"unsigned_urls"}, validateVideoResponse)
}

// VideoContent invokes the video_content handler for one upstream job ID
// and asset index. Video bytes cross the boundary base64-encoded like
// speech audio. A missing handler reports ErrHandlerNotFound.
func (s *Service) VideoContent(
	goCtx context.Context,
	handlerMetadata HandlerMeta,
	model models.ModelId,
	upstreamJobID string,
	index int,
) (*models.VideoContentResponse, error) {
	response, _, err := s.VideoContentRouted(goCtx, handlerMetadata, model, upstreamJobID, index)
	return response, err
}

// VideoContentRouted is VideoContent plus the redacted proxy host:port of
// the call ("" = direct).
func (s *Service) VideoContentRouted(
	goCtx context.Context,
	handlerMetadata HandlerMeta,
	model models.ModelId,
	upstreamJobID string,
	index int,
) (*models.VideoContentResponse, string, error) {
	out, proxy, err := callAndDecode(s, goCtx, handlerMetadata, "video_content", func(L *lua.LState) {
		L.Push(ctxTable(L, "", handlerMetadata.ProviderConfig))
		L.Push(videoJobTable(L, model, upstreamJobID, index, true))
	}, nil, func(payload *videoContentPayload) error {
		video, derr := base64.StdEncoding.DecodeString(payload.VideoB64)
		if derr != nil {
			return errInvalidVideoB64
		}
		if len(video) == 0 {
			return errEmptyVideo
		}
		return nil
	})
	if err != nil {
		return nil, proxy, err
	}
	video, _ := base64.StdEncoding.DecodeString(out.VideoB64)
	return &models.VideoContentResponse{Video: video, ContentType: out.ContentType}, proxy, nil
}

// ValidateCredentials calls validate_credentials. Missing handler accepts
// any data. Returns valid=false with a ProviderError on rejection.
func (s *Service) ValidateCredentials(typeKey string, data map[string]any) (bool, error) {
	record, err := s.Lookup(typeKey)
	if err != nil {
		s.logLookupFailed(typeKey, "validate_credentials", err)
		return false, err
	}
	if data == nil {
		data = map[string]any{}
	}
	valid := true
	found, err := s.handlerCall(context.Background(), record, typeKey, "validate_credentials", func(L *lua.LState) {
		L.Push(toLuaValue(L, data))
	}, 2, func(L *lua.LState) error {
		result, returnError := splitReturn(L)
		if b, ok := result.(lua.LBool); ok && !bool(b) {
			valid = false
			if returnError == lua.LNil {
				return &models.ProviderError{StatusCode: 400, Code: "invalid_request_error", Message: "credentials rejected"}
			}
			return s.contractErrOrInternal(record, typeKey, returnError)
		}
		if b, ok := result.(lua.LBool); ok && bool(b) {
			return s.contractErrOrInternal(record, typeKey, returnError)
		}
		if result != lua.LNil {
			if _, ok := result.(lua.LBool); !ok {
				s.recordCrash(record.ID, typeKey, "validate_credentials must return boolean")
				return &models.PluginInternalError{PluginID: record.ID, TypeKey: typeKey, Cause: "validate_credentials must return boolean"}
			}
		}
		return s.contractErrOrInternal(record, typeKey, returnError)
	}, nil)
	if err != nil {
		return false, err
	}
	if !found {
		return true, nil
	}
	return valid, nil
}

// GetModelInfos calls get_model_infos with the provider config in ctx.
// The plugin selects its own credential for discovery from the provider's
// scope: providerID threads into the call so credentials.list() sees the
// provider's rows. Missing handler reports ErrHandlerNotFound so callers
// apply the fixed fallback.
func (s *Service) GetModelInfos(
	goCtx context.Context,
	providerID string,
	typeKey string,
	providerConfig map[string]any,
) ([]models.ModelInfo, error) {
	record, err := s.Lookup(typeKey)
	if err != nil {
		s.logLookupFailed(typeKey, "get_model_infos", err)
		return nil, err
	}
	var modelInfos []models.ModelInfo
	handlerMetadata := HandlerMeta{ProviderID: providerID, TypeKey: typeKey, ProviderConfig: providerConfig}
	found, _, callError := s.handlerCallRouted(goCtx, record, handlerMetadata, "get_model_infos", func(L *lua.LState) {
		L.Push(ctxTable(L, "", providerConfig))
	}, 2, func(L *lua.LState) error {
		result, returnError := splitReturn(L)
		if cerr := s.contractErrOrInternal(record, typeKey, returnError); cerr != nil {
			return cerr
		}
		if result == lua.LNil {
			modelInfos = []models.ModelInfo{}
			return nil
		}
		raw, merr := marshalLua(result)
		if merr != nil {
			s.recordCrash(record.ID, typeKey, "get_model_infos encode failure: "+merr.Error())
			return &models.PluginInternalError{PluginID: record.ID, TypeKey: typeKey, Cause: "encode model infos: " + merr.Error()}
		}
		var out []models.ModelInfo
		if uerr := unmarshalTo(raw, &out); uerr != nil {
			s.recordCrash(record.ID, typeKey, "get_model_infos schema violation: "+uerr.Error())
			return &models.PluginInternalError{PluginID: record.ID, TypeKey: typeKey, Cause: "get_model_infos schema violation: " + uerr.Error()}
		}
		modelInfos = out
		return nil
	})
	if callError != nil {
		return nil, callError
	}
	if !found {
		return nil, &notFoundError{PluginID: record.ID, TypeKey: typeKey, Handler: "get_model_infos"}
	}
	modelInfos = applyModelSpecifications(modelInfos, record.ModelSpecifications[typeKey])
	if modelInfos == nil {
		modelInfos = []models.ModelInfo{}
	}
	return modelInfos, nil
}

// Schema serves static UI tables from the stored record: credential_schema,
// config_schema, settings_schema or proxy_schema. Missing tables report
// ErrHandlerNotFound so callers hide the surface instead of rendering it.
// Stored widget keys resolve through the type locales for locale.
func (s *Service) Schema(typeKey, kind, locale string) ([]*models.UINode, error) {
	switch kind {
	case "credential_schema", "config_schema", "settings_schema", "proxy_schema":
	default:
		return nil, fmt.Errorf("unknown schema %q", kind)
	}
	record, err := s.Lookup(typeKey)
	if err != nil {
		s.logLookupFailed(typeKey, kind, err)
		return nil, err
	}
	nodes, ok := record.Schemas[typeKey][kind]
	if !ok {
		return nil, &notFoundError{PluginID: record.ID, TypeKey: typeKey, Handler: kind}
	}
	return ResolveUINodes(nodes, record.Locales[typeKey], locale), nil
}

// AuthInitiate calls auth_initiate. Missing handler reports ErrHandlerNotFound
// so the dashboard uses the single-step credential_schema form. Render trees
// resolve through the type locales for locale.
func (s *Service) AuthInitiate(goCtx context.Context, typeKey, flowID string, providerConfig map[string]any, locale string) (*models.AuthFlowResult, error) {
	record, err := s.Lookup(typeKey)
	if err != nil {
		s.logLookupFailed(typeKey, "auth_initiate", err)
		return nil, err
	}
	var result *models.AuthFlowResult
	found, err := s.handlerCall(goCtx, record, typeKey, "auth_initiate", func(L *lua.LState) {
		L.Push(ctxTable(L, flowID, providerConfig))
	}, 1, func(L *lua.LState) error {
		parsed, perr := parseAuthResult(L.Get(-1))
		if perr != nil {
			s.recordCrash(record.ID, typeKey, "auth_initiate result violation: "+perr.Error())
			return &models.PluginInternalError{PluginID: record.ID, TypeKey: typeKey, Cause: "auth_initiate result violation: " + perr.Error()}
		}
		result = parsed
		return nil
	}, nil)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, &notFoundError{PluginID: record.ID, TypeKey: typeKey, Handler: "auth_initiate"}
	}
	if result != nil && len(result.Render) > 0 {
		result.Render = ResolveUINodes(result.Render, record.Locales[typeKey], locale)
	}
	return result, nil
}

// AuthStep calls auth_step with {action, values}. Render trees resolve
// through the type locales for locale.
func (s *Service) AuthStep(goCtx context.Context, typeKey, flowID, action string, values map[string]any, providerConfig map[string]any, locale string) (*models.AuthFlowResult, error) {
	record, err := s.Lookup(typeKey)
	if err != nil {
		s.logLookupFailed(typeKey, "auth_step", err)
		return nil, err
	}
	if values == nil {
		values = map[string]any{}
	}
	var result *models.AuthFlowResult
	found, err := s.handlerCall(goCtx, record, typeKey, "auth_step", func(L *lua.LState) {
		L.Push(ctxTable(L, flowID, providerConfig))
		input := L.NewTable()
		input.RawSetString("action", lua.LString(action))
		input.RawSetString("values", toLuaValue(L, values))
		L.Push(input)
	}, 1, func(L *lua.LState) error {
		parsed, perr := parseAuthResult(L.Get(-1))
		if perr != nil {
			s.recordCrash(record.ID, typeKey, "auth_step result violation: "+perr.Error())
			return &models.PluginInternalError{PluginID: record.ID, TypeKey: typeKey, Cause: "auth_step result violation: " + perr.Error()}
		}
		result = parsed
		return nil
	}, nil)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, &notFoundError{PluginID: record.ID, TypeKey: typeKey, Handler: "auth_step"}
	}
	if result != nil && len(result.Render) > 0 {
		result.Render = ResolveUINodes(result.Render, record.Locales[typeKey], locale)
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
// one qualified key per (record, declared name), first-seen order. Reads
// storage: source-only plugins carry no type key, so the type-keyed
// in-memory registry never holds them and a registry read would report no
// sources after every startup.
func (s *Service) ProxySourceKeys() ([]string, error) {
	records, err := s.ListSQLite(context.Background())
	if err != nil {
		return nil, err
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
	var keys []string
	seen := map[string]bool{}
	for _, record := range records {
		for _, k := range record.ProxySourceKeys {
			q := QualifiedSourceKey(record.ID, k)
			if !seen[q] {
				seen[q] = true
				keys = append(keys, q)
			}
		}
	}
	return keys, nil
}

// FetchProxies invokes the fetch_proxies handler of a proxy source plugin.
// sourceKey is qualified; the owning record is resolved exactly, and the
// routed call uses the declared name the plugin registered.
func (s *Service) FetchProxies(goCtx context.Context, sourceKey string) ([]proxypool.Candidate, error) {
	records, err := s.ListSQLite(goCtx)
	if err != nil {
		return nil, err
	}
	var record *PluginRecord
	declared := ""
	for _, pluginRecord := range records {
		for _, k := range pluginRecord.ProxySourceKeys {
			if QualifiedSourceKey(pluginRecord.ID, k) == sourceKey {
				record = pluginRecord
				declared = k
				break
			}
		}
	}
	if record == nil {
		missing := fmt.Errorf("proxy source %q not found", sourceKey)
		s.logLookupFailed(sourceKey, "fetch_proxies", missing)
		return nil, missing
	}
	var candidates []proxypool.Candidate
	found, err := s.handlerCall(goCtx, record, declared, "fetch_proxies", nil, 1, func(L *lua.LState) error {
		result := L.Get(-1)
		if result == lua.LNil {
			candidates = []proxypool.Candidate{}
			return nil
		}
		raw, merr := marshalLua(result)
		if merr != nil {
			s.recordCrash(record.ID, declared, "fetch_proxies encode failure: "+merr.Error())
			return &models.PluginInternalError{PluginID: record.ID, Cause: "encode proxies: " + merr.Error()}
		}
		// Feeds emit [{url=, country=}] or [{protocol=, host=, port=,
		// country=}]: accept both shapes, drop rows without an address.
		var rows []struct {
			URL      string `json:"url"`
			Protocol string `json:"protocol"`
			Host     string `json:"host"`
			Port     int    `json:"port"`
			Country  string `json:"country"`
		}
		if uerr := unmarshalTo(raw, &rows); uerr != nil {
			s.recordCrash(record.ID, sourceKey, "fetch_proxies schema violation: "+uerr.Error())
			return &models.PluginInternalError{PluginID: record.ID, Cause: "fetch_proxies schema violation: " + uerr.Error()}
		}
		candidates = make([]proxypool.Candidate, 0, len(rows))
		for _, row := range rows {
			u := row.URL
			if u == "" && row.Host != "" && row.Port > 0 {
				proto := row.Protocol
				if proto == "" {
					proto = "http"
				}
				u = proto + "://" + row.Host + ":" + fmt.Sprintf("%d", row.Port)
			}
			if u != "" {
				candidates = append(candidates, proxypool.Candidate{URL: u, Country: row.Country})
			}
		}
		return nil
	}, nil)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, &notFoundError{PluginID: record.ID, TypeKey: sourceKey, Handler: "fetch_proxies"}
	}
	return candidates, nil
}

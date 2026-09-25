package luaplugin

import (
	"context"
	"errors"

	"github.com/TheSlopMachine/llm-router/internal/models"
	lua "github.com/yuin/gopher-lua"
)

// errEmptyChoices marks a completion response without choices.
var errEmptyChoices = errors.New("empty choices")

// errInvalidAudioB64 marks a speech payload with undecodable audio.
var errInvalidAudioB64 = errors.New("audio_b64 is not valid base64")

// errEmptyAudio marks a speech payload with zero audio bytes.
var errEmptyAudio = errors.New("returned empty audio")

// errEmptyImageData marks an image response without data.
var errEmptyImageData = errors.New("empty data")

// errEmbedLengthMismatch marks an embed response miscounted against inputs.
var errEmbedLengthMismatch = errors.New("data length does not match input length")

// errEmptyEmbeddingVector marks an embed response with an empty vector.
var errEmptyEmbeddingVector = errors.New("empty embedding vector")

// speechPayload is the wire shape of the speech handler result.
type speechPayload struct {
	AudioB64 string `json:"audio_b64"`
	Format   string `json:"format"`
}

// decodeHandlerResult validates one handler return value: contract error,
// nil guard, Lua→JSON marshal, schema unmarshal with empty-object
// normalization, and handler-specific validation. Every schema violation is
// recorded as a plugin crash.
func decodeHandlerResult[T any](
	s *Service,
	rec *PluginRecord,
	typeKey, handler string,
	result, rawErr lua.LValue,
	emptyKeys []string,
	validate func(*T) error,
) (*T, error) {
	if cerr := s.contractErrOrInternal(rec, typeKey, rawErr); cerr != nil {
		return nil, cerr
	}
	if result == lua.LNil {
		return nil, &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: handler + " returned nil result"}
	}
	raw, merr := marshalLua(result)
	if merr != nil {
		return nil, &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "encode response: " + merr.Error()}
	}
	out := new(T)
	if uerr := unmarshalTo(normalizeEmptyObjects(raw, emptyKeys...), out); uerr != nil {
		return nil, s.schemaViolation(rec, typeKey, handler, uerr.Error())
	}
	if validate != nil {
		if verr := validate(out); verr != nil {
			return nil, s.schemaViolation(rec, typeKey, handler, verr.Error())
		}
	}
	return out, nil
}

// schemaViolation records a crash and wraps the cause as a plugin error.
func (s *Service) schemaViolation(rec *PluginRecord, typeKey, handler, cause string) error {
	msg := handler + " schema violation: " + cause
	s.recordCrash(rec.ID, typeKey, msg)
	return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: msg}
}

// callAndDecode runs one request handler (Lookup, routed call, decode) and
// maps a missing handler to ErrHandlerNotFound via notFoundError. It also
// returns the redacted proxy host:port of the call ("" = direct).
func callAndDecode[T any](
	s *Service,
	goCtx context.Context,
	meta HandlerMeta,
	handler string,
	pushArgs func(*lua.LState),
	emptyKeys []string,
	validate func(*T) error,
) (*T, string, error) {
	typeKey := meta.TypeKey
	rec, err := s.Lookup(typeKey)
	if err != nil {
		return nil, "", err
	}
	var out *T
	found, proxy, callErr := s.handlerCallRouted(goCtx, rec, meta, handler, pushArgs, 2, func(L *lua.LState) error {
		result, rawErr := splitReturn(L)
		decoded, derr := decodeHandlerResult(s, rec, typeKey, handler, result, rawErr, emptyKeys, validate)
		if derr != nil {
			return derr
		}
		out = decoded
		return nil
	})
	if callErr != nil {
		return nil, proxy, callErr
	}
	if !found {
		return nil, proxy, &notFoundError{PluginID: rec.ID, TypeKey: typeKey, Handler: handler}
	}
	return out, proxy, nil
}

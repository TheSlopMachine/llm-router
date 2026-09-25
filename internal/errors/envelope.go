package errors

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ParseEnvelope extracts code/type/message from an OpenAI-style error
// envelope: {"error":{"code","type","message"}}. Unknown shapes yield empty
// values and callers fall back to status-based mapping. Message text never
// decides the error type; MapUpstream owns that mapping.
func ParseEnvelope(body string) (code, errType, message string) {
	var envelope struct {
		Error struct {
			Code    any `json:"code"`
			Type    any `json:"type"`
			Message any `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		return "", "", ""
	}
	return envelopeString(envelope.Error.Code), envelopeString(envelope.Error.Type), envelopeString(envelope.Error.Message)
}

func envelopeString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case float64:
		return strings.TrimSuffix(fmt.Sprintf("%v", s), ".0")
	default:
		return ""
	}
}

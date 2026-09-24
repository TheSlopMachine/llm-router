package v1

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// maxJSONBodyBytes caps a single JSON request body. Large inputs arrive as
// multipart uploads (audio) or b64_json responses, never as raw JSON.
const maxJSONBodyBytes = 8 << 20

// errBodyTooLarge reports a JSON body over maxJSONBodyBytes.
var errBodyTooLarge = errors.New("request body too large")

// decodeJSONBody parses a JSON request body under the shared size limit.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return errBodyTooLarge
		}
		return err
	}
	return nil
}

// writeDecodeError maps body decode failures: oversized bodies are 413,
// everything else keeps the existing 400 shape.
func (h *Handler) writeDecodeError(w http.ResponseWriter, err error) {
	if errors.Is(err, errBodyTooLarge) {
		h.writeError(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "request body too large", nil)
		return
	}
	h.writeError(w, http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("malformed request body: %s", err), nil)
}

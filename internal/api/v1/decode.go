package v1

import (
	"encoding/json"
	"errors"
	"net/http"
)

// maxJSONBodyBytes caps a single JSON request body. Large inputs arrive as
// multipart uploads (audio) or b64_json responses, never as raw JSON.
const maxJSONBodyBytes = 8 << 20

// errBodyTooLarge reports a JSON body over maxJSONBodyBytes.
var errBodyTooLarge = errors.New("request body too large")

// decodeJSONBody parses a JSON request body under the shared size limit.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return errBodyTooLarge
		}
		return err
	}
	return nil
}

// isBodyTooLarge reports an over-limit JSON body for negotiated envelopes.
func isBodyTooLarge(err error) bool {
	return errors.Is(err, errBodyTooLarge)
}

// writeDecodeError maps body decode failures in the OpenAI envelope.
// OpenAI-only callers have no negotiated shape, so delegate to the
// negotiated variant with a headerless request: empty headers force the
// OpenAI branch, keeping status codes and envelope identical. The real
// request still drives the log keys.
func (h *Handler) writeDecodeError(w http.ResponseWriter, r *http.Request, err error) {
	h.logDecodeFailure(r, err)
	h.writeCompatDecodeErrorWithoutLog(w, &http.Request{}, err)
}

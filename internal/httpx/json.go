package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// ErrorBody is the uniform JSON error envelope.
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail describes an error without leaking internals.
type ErrorDetail struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId,omitempty"`
}

// WriteJSON writes v as JSON with the given status. API responses are never cached.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Error("encode response", "error", err)
		status = http.StatusInternalServerError
		b = []byte(`{"error":{"code":"internal","message":"internal server error"}}`)
	}
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(append(b, '\n'))
}

// WriteError writes the uniform error envelope including the request ID.
func WriteError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	WriteJSON(w, status, ErrorBody{Error: ErrorDetail{Code: code, Message: message, RequestID: RequestIDFrom(r.Context())}})
}

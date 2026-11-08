package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"

	"github.com/yesoreyeram/dv/internal/httpx"
	"github.com/yesoreyeram/dv/internal/query"
)

// dataSources lists provisioned instances, identity only (FR-017 AC3).
func (s *Server) dataSources(w http.ResponseWriter, r *http.Request) {
	if s.query == nil {
		s.notFound(w, r)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string][]query.Summary{"datasources": s.query.DataSources()})
}

// runQuery executes a batch of queries (FR-016).
func (s *Server) runQuery(w http.ResponseWriter, r *http.Request) {
	if s.query == nil {
		s.notFound(w, r)
		return
	}
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "application/json" {
		httpx.WriteError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
		return
	}
	req, status, err := decodeQueryRequest(r.Body)
	if err != nil {
		httpx.WriteError(w, r, status, "invalid_request", err.Error())
		return
	}
	resp, status, err := s.query.Execute(r.Context(), req)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	httpx.WriteJSON(w, status, resp)
}

// decodeQueryRequest decodes strictly: unknown fields and trailing data are rejected,
// and a body over the MaxBody limit is 413 (FR-016 AC1, NFR-SEC-009).
func decodeQueryRequest(body io.Reader) (query.Request, int, error) {
	var req query.Request
	b, err := io.ReadAll(body)
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return req, http.StatusRequestEntityTooLarge, fmt.Errorf("request body exceeds %d bytes", tooBig.Limit)
		}
		return req, http.StatusBadRequest, errors.New("could not read the request body")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return req, http.StatusBadRequest, fmt.Errorf("request body is not a valid query request: %w", err)
	}
	if dec.More() {
		return req, http.StatusBadRequest, errors.New("unexpected data after the JSON object")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return req, http.StatusBadRequest, errors.New("unexpected data after the JSON object")
	}
	return req, 0, nil
}

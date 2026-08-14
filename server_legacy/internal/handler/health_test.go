package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHealthReturnsOK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	Health(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&body)
	assert.Equal(t, "ok", body["status"])
}

package handler

import (
	"encoding/json"
	"net/http"

	"github.com/tokfinity/infera/internal/auth"
)

type AuthHandler struct {
	m *auth.Manager
}

func NewAuthHandler(m *auth.Manager) *AuthHandler { return &AuthHandler{m: m} }

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if !h.m.Verify(body.Password) {
		writeErr(w, http.StatusUnauthorized, "invalid password")
		return
	}
	h.m.SetLogin(w)
	writeJSON(w, http.StatusOK, map[string]bool{"logged_in": true})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	h.m.Clear(w)
	writeJSON(w, http.StatusOK, map[string]bool{"logged_in": false})
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"logged_in": h.m.IsLoggedIn(r)})
}

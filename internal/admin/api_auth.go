package admin

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if req.Username != s.cfg.Username || req.Password != s.cfg.Password {
		http.Error(w, `{"ok":false,"error":"invalid credentials"}`, http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleChangeCredentials(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if req.Username == "" || req.Password == "" {
		http.Error(w, "username and password are required", http.StatusBadRequest)
		return
	}
	s.cfg.Username = req.Username
	s.cfg.Password = req.Password
	if err := WriteAdminEnv(s.cfg.ConfigDir, s.cfg.Port, s.cfg.Username, s.cfg.Password, s.cfg.Domain, s.cfg.SubPort); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleChangeToken is kept for backward compatibility but now rotates the password.
func (s *Server) handleChangeToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	newPass := hex.EncodeToString(b)
	s.cfg.Password = newPass
	if err := WriteAdminEnv(s.cfg.ConfigDir, s.cfg.Port, s.cfg.Username, s.cfg.Password, s.cfg.Domain, s.cfg.SubPort); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "password": newPass})
}

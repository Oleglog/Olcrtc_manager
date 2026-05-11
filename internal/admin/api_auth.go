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
		Token string `json:"token"`
	}
	if err := readJSON(r, &req); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if req.Token != s.cfg.Token {
		http.Error(w, `{"ok":false,"error":"invalid token"}`, http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleChangeToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		NewToken string `json:"new_token"`
	}
	if err := readJSON(r, &req); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	tok := req.NewToken
	if tok == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		tok = hex.EncodeToString(b)
	}
	s.cfg.Token = tok
	if err := WriteAdminEnv(s.cfg.ConfigDir, s.cfg.Port, tok, s.cfg.Domain, s.cfg.SubPort); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "token": tok})
}

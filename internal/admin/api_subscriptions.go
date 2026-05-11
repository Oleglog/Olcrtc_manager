package admin

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleSubs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.proxySubRequest(w, r, "/api/subscriptions")
	case http.MethodPost:
		s.proxySubRequest(w, r, "/api/subscriptions")
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSubsSlug(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/subs")
	target := "/api/subscriptions" + path
	s.proxySubRequest(w, r, target)
}

func (s *Server) handlePublicSub(w http.ResponseWriter, r *http.Request) {
	// Proxy /sub/{slug} to subscription server.
	target := r.URL.Path
	if r.URL.RawQuery != "" {
		target = target + "?" + r.URL.RawQuery
	}
	s.proxySubRequestInternal(w, r, target)
}

func (s *Server) proxySubRequestInternal(w http.ResponseWriter, r *http.Request, target string) {
	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	urlStr := "http://127.0.0.1:" + itoa(s.cfg.SubPort) + target
	req, err := http.NewRequest(r.Method, urlStr, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	req.Header = r.Header.Clone()
	s.doProxy(w, req)
}

func (s *Server) proxySubRequest(w http.ResponseWriter, r *http.Request, targetPath string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	urlStr := "http://127.0.0.1:" + itoa(s.cfg.SubPort) + targetPath
	req, err := http.NewRequest(r.Method, urlStr, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	req.Header = r.Header.Clone()
	s.doProxy(w, req)
}

func (s *Server) doProxy(w http.ResponseWriter, req *http.Request) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Subscription API unreachable: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func itoa(n int) string {
	var buf [20]byte
	i := len(buf)
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

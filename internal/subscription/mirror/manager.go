// Package mirror uploads encrypted subscription mirrors to external storage.
package mirror

import (
    "bytes"
    "context"
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "encoding/base64"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "path"
    "strings"
    "time"
)

// Config controls encrypted mirror publishing.
type Config struct {
    Enabled bool
    Provider string
    OAuthToken string
    BasePath string
}

// Result describes a published mirror.
type Result struct {
    Type string `json:"type"`
    URL string `json:"url"`
    Key string `json:"key"`
    UpdatedAt time.Time `json:"updated_at"`
}

type Manager struct {
    cfg Config
    client *http.Client
}

func New(cfg Config) *Manager {
    if cfg.Provider == "" { cfg.Provider = "yandex_disk" }
    if cfg.BasePath == "" { cfg.BasePath = "/olcrtc/subscriptions" }
    return &Manager{cfg: cfg, client: &http.Client{Timeout: 30 * time.Second}}
}

func (m *Manager) Enabled() bool { return m != nil && m.cfg.Enabled && m.cfg.Provider == "yandex_disk" && m.cfg.OAuthToken != "" }

func GenerateKey() (string, error) {
    b := make([]byte, 32)
    if _, err := rand.Read(b); err != nil { return "", err }
    return base64.RawURLEncoding.EncodeToString(b), nil
}

type envelope struct {
    Type string `json:"type"`
    Version int `json:"v"`
    Alg string `json:"alg"`
    Nonce string `json:"nonce"`
    Ciphertext string `json:"ciphertext"`
}

// EncryptLines returns an encrypted mirror JSON for the same raw text format as /sub/{slug}.
func EncryptLines(lines []string, keyB64 string) ([]byte, error) {
    key, err := base64.RawURLEncoding.DecodeString(keyB64)
    if err != nil { return nil, fmt.Errorf("decode mirror key: %w", err) }
    if len(key) != 32 { return nil, fmt.Errorf("mirror key must be 32 bytes, got %d", len(key)) }
    block, err := aes.NewCipher(key)
    if err != nil { return nil, err }
    gcm, err := cipher.NewGCM(block)
    if err != nil { return nil, err }
    nonce := make([]byte, gcm.NonceSize())
    if _, err := rand.Read(nonce); err != nil { return nil, err }
    plain := []byte(strings.Join(lines, "\n"))
    if len(lines) > 0 { plain = append(plain, '\n') }
    out := envelope{Type: "olcrtc-sub-mirror", Version: 1, Alg: "AES-256-GCM", Nonce: base64.RawURLEncoding.EncodeToString(nonce), Ciphertext: base64.RawURLEncoding.EncodeToString(gcm.Seal(nil, nonce, plain, nil))}
    return json.MarshalIndent(out, "", "  ")
}

func (m *Manager) Publish(ctx context.Context, slug string, lines []string, keyB64 string) (string, error) {
    if !m.Enabled() { return "", errors.New("mirror manager is disabled") }
    data, err := EncryptLines(lines, keyB64)
    if err != nil { return "", err }
    filePath := path.Join(m.cfg.BasePath, slug+".json")
    if !strings.HasPrefix(filePath, "/") { filePath = "/" + filePath }
    if err := m.ensureDirs(ctx, path.Dir(filePath)); err != nil { return "", err }
    href, err := m.uploadHref(ctx, filePath)
    if err != nil { return "", err }
    if err := m.put(ctx, href, data); err != nil { return "", err }
    if err := m.publishPath(ctx, filePath); err != nil { return "", err }
    publicURL, err := m.publicURL(ctx, filePath)
    if err != nil { return "", err }
    return publicURL, nil
}

func (m *Manager) ensureDirs(ctx context.Context, dir string) error {
    dir = strings.Trim(dir, "/")
    if dir == "" { return nil }
    cur := ""
    for _, part := range strings.Split(dir, "/") {
        cur += "/" + part
        if err := m.mkdir(ctx, cur); err != nil { return err }
    }
    return nil
}

func (m *Manager) mkdir(ctx context.Context, p string) error {
    u := "https://cloud-api.yandex.net/v1/disk/resources?path=" + url.QueryEscape(p)
    req, _ := http.NewRequestWithContext(ctx, http.MethodPut, u, nil)
    m.auth(req)
    resp, err := m.client.Do(req)
    if err != nil { return err }
    defer resp.Body.Close()
    if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusConflict || resp.StatusCode == http.StatusOK { return nil }
    body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
    return fmt.Errorf("yandex mkdir %s: %s: %s", p, resp.Status, string(body))
}

func (m *Manager) uploadHref(ctx context.Context, p string) (string, error) {
    u := "https://cloud-api.yandex.net/v1/disk/resources/upload?overwrite=true&path=" + url.QueryEscape(p)
    req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
    m.auth(req)
    resp, err := m.client.Do(req)
    if err != nil { return "", err }
    defer resp.Body.Close()
    if resp.StatusCode < 200 || resp.StatusCode >= 300 { body,_:=io.ReadAll(io.LimitReader(resp.Body,4096)); return "", fmt.Errorf("yandex upload href: %s: %s", resp.Status, string(body)) }
    var v struct{ Href string `json:"href"` }
    if err := json.NewDecoder(resp.Body).Decode(&v); err != nil { return "", err }
    if v.Href == "" { return "", errors.New("empty yandex upload href") }
    return v.Href, nil
}

func (m *Manager) put(ctx context.Context, href string, data []byte) error {
    req, _ := http.NewRequestWithContext(ctx, http.MethodPut, href, bytes.NewReader(data))
    req.Header.Set("Content-Type", "application/json")
    resp, err := m.client.Do(req)
    if err != nil { return err }
    defer resp.Body.Close()
    if resp.StatusCode < 200 || resp.StatusCode >= 300 { body,_:=io.ReadAll(io.LimitReader(resp.Body,4096)); return fmt.Errorf("yandex upload: %s: %s", resp.Status, string(body)) }
    return nil
}

func (m *Manager) publishPath(ctx context.Context, p string) error {
    u := "https://cloud-api.yandex.net/v1/disk/resources/publish?path=" + url.QueryEscape(p)
    req, _ := http.NewRequestWithContext(ctx, http.MethodPut, u, nil)
    m.auth(req)
    resp, err := m.client.Do(req)
    if err != nil { return err }
    defer resp.Body.Close()
    if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusConflict { return nil }
    body,_:=io.ReadAll(io.LimitReader(resp.Body,4096)); return fmt.Errorf("yandex publish: %s: %s", resp.Status, string(body))
}

func (m *Manager) publicURL(ctx context.Context, p string) (string, error) {
    u := "https://cloud-api.yandex.net/v1/disk/resources?fields=public_url&path=" + url.QueryEscape(p)
    req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
    m.auth(req)
    resp, err := m.client.Do(req)
    if err != nil { return "", err }
    defer resp.Body.Close()
    if resp.StatusCode < 200 || resp.StatusCode >= 300 { body,_:=io.ReadAll(io.LimitReader(resp.Body,4096)); return "", fmt.Errorf("yandex public url: %s: %s", resp.Status, string(body)) }
    var v struct{ PublicURL string `json:"public_url"` }
    if err := json.NewDecoder(resp.Body).Decode(&v); err != nil { return "", err }
    if v.PublicURL == "" { return "", errors.New("empty yandex public_url") }
    return v.PublicURL, nil
}

func (m *Manager) auth(req *http.Request) { req.Header.Set("Authorization", "OAuth "+m.cfg.OAuthToken) }

// Test performs a tiny upload+delete probe to verify OAuth token and base path.
// ponytail: reuses existing Publish primitives; no new HTTP plumbing.
func (m *Manager) Test(ctx context.Context) error {
    if m.cfg.OAuthToken == "" { return errors.New("oauth token empty") }
    probe := path.Join(m.cfg.BasePath, ".olcrtc-ping-"+time.Now().Format("20060102-150405")+".json")
    if !strings.HasPrefix(probe, "/") { probe = "/" + probe }
    if err := m.ensureDirs(ctx, path.Dir(probe)); err != nil { return err }
    href, err := m.uploadHref(ctx, probe)
    if err != nil { return err }
    if err := m.put(ctx, href, []byte("{\"ping\":true}")); err != nil { return err }
    _ = m.deletePath(ctx, probe)
    return nil
}

func (m *Manager) deletePath(ctx context.Context, p string) error {
    u := "https://cloud-api.yandex.net/v1/disk/resources?path=" + url.QueryEscape(p)
    req, _ := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
    m.auth(req)
    resp, err := m.client.Do(req)
    if err != nil { return err }
    defer resp.Body.Close()
    if resp.StatusCode == http.StatusNotFound || resp.StatusCode < 300 { return nil }
    body,_:=io.ReadAll(io.LimitReader(resp.Body,4096))
    return fmt.Errorf("yandex delete %s: %s: %s", p, resp.Status, string(body))
}

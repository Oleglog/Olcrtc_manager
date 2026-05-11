package admin

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/acme/autocert"
)

func (s *Server) buildTLS(ctx context.Context) (*tls.Config, error) {
	if s.cfg.Domain != "" {
		return s.buildACMETLS(ctx)
	}
	return s.buildSelfSignedTLS()
}

func (s *Server) buildSelfSignedTLS() (*tls.Config, error) {
	dir := s.cfg.TLSDir
	caCertPath := filepath.Join(dir, "ca.crt")
	caKeyPath := filepath.Join(dir, "ca.key")
	serverCertPath := filepath.Join(dir, "server.crt")
	serverKeyPath := filepath.Join(dir, "server.key")

	// Ensure directory exists.
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	// Generate CA if not exists.
	if _, err := os.Stat(caCertPath); os.IsNotExist(err) {
		if err := generateCA(caCertPath, caKeyPath); err != nil {
			return nil, fmt.Errorf("generate CA: %w", err)
		}
	}

	// Check if server cert exists and matches current IP.
	needNewServer := false
	if _, err := os.Stat(serverCertPath); os.IsNotExist(err) {
		needNewServer = true
	} else {
		certPEM, err := os.ReadFile(serverCertPath)
		if err != nil {
			needNewServer = true
		} else {
			block, _ := pem.Decode(certPEM)
			if block == nil {
				needNewServer = true
			} else {
				cert, err := x509.ParseCertificate(block.Bytes)
				if err != nil {
					needNewServer = true
				} else {
					// Check SANs.
					hasIP := false
					for _, ip := range cert.IPAddresses {
						if ip.String() == s.cfg.PublicIP {
							hasIP = true
							break
						}
					}
					if !hasIP {
						needNewServer = true
					}
					// Check expiry.
					if time.Until(cert.NotAfter) < 7*24*time.Hour {
						needNewServer = true
					}
				}
			}
		}
	}

	if needNewServer {
		if err := generateServerCert(caCertPath, caKeyPath, serverCertPath, serverKeyPath, s.cfg.PublicIP); err != nil {
			return nil, fmt.Errorf("generate server cert: %w", err)
		}
	}

	cert, err := tls.LoadX509KeyPair(serverCertPath, serverKeyPath)
	if err != nil {
		return nil, err
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
	}, nil
}

func generateCA(certPath, keyPath string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"olcRTC Admin CA"},
			CommonName:   "olcRTC Admin CA",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}

	certOut, err := os.OpenFile(certPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return err
	}

	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer keyOut.Close()
	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	return pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
}

func generateServerCert(caCertPath, caKeyPath, certPath, keyPath, publicIP string) error {
	caCertPEM, err := os.ReadFile(caCertPath)
	if err != nil {
		return err
	}
	caBlock, _ := pem.Decode(caCertPEM)
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		return err
	}

	caKeyPEM, err := os.ReadFile(caKeyPath)
	if err != nil {
		return err
	}
	caKeyBlock, _ := pem.Decode(caKeyPEM)
	caKey, err := x509.ParseECPrivateKey(caKeyBlock.Bytes)
	if err != nil {
		return err
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			Organization: []string{"olcRTC Admin"},
			CommonName:   "olcRTC Admin Server",
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP(publicIP), net.ParseIP("127.0.0.1")},
		DNSNames:    []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		return err
	}

	certOut, err := os.OpenFile(certPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return err
	}

	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer keyOut.Close()
	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	return pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
}

func (s *Server) buildACMETLS(ctx context.Context) (*tls.Config, error) {
	dir := filepath.Join(s.cfg.TLSDir, "acme")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}

	m := &autocert.Manager{
		Cache:      autocert.DirCache(dir),
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(s.cfg.Domain),
		Email:      s.cfg.ACMEEmail,
	}

	return m.TLSConfig(), nil
}

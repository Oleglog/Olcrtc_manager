package admin

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

var preferredPorts = []int{8443, 9443, 8080, 3000, 4443}

// FindFreePort scans preferred ports and returns the first free one.
func FindFreePort() (int, error) {
	for _, p := range preferredPorts {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", p))
		if err == nil {
			_ = ln.Close()
			return p, nil
		}
	}
	// Fallback: bind to :0 and see what we got.
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// IsPortFree checks if a port is free on the given host.
func IsPortFree(host string, port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// HTTPGetWithTimeout performs a simple HTTP GET with timeout.
func HTTPGetWithTimeout(url string, timeout time.Duration) ([]byte, error) {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

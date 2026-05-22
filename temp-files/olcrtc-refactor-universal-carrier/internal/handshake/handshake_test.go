package handshake

import (
	"errors"
	"io"
	"net"
	"strings"
	"testing"
)

const testSessionID = "sess-42"

var errNope = errors.New("nope")

func pair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() {
		_ = a.Close()
		_ = b.Close()
	})
	return a, b
}

func TestHandshakeRoundTrip(t *testing.T) {
	cConn, sConn := pair(t)

	go func() {
		hello, sid, err := Server(sConn, func(deviceID string, claims map[string]any) (string, error) {
			if deviceID != "dev-1" {
				t.Errorf("device id = %q", deviceID)
			}
			if claims["plan"] != "pro" {
				t.Errorf("claims = %v", claims)
			}
			return testSessionID, nil
		})
		if err != nil {
			t.Errorf("Server: %v", err)
		}
		if hello.DeviceID != "dev-1" || sid != testSessionID {
			t.Errorf("Server returned hello=%+v sid=%q", hello, sid)
		}
	}()

	sid, err := Client(cConn, "dev-1", map[string]any{"plan": "pro"})
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	if sid != testSessionID {
		t.Fatalf("session id = %q, want sess-42", sid)
	}
}

func TestHandshakeRejected(t *testing.T) {
	cConn, sConn := pair(t)

	go func() {
		_, _, _ = Server(sConn, func(string, map[string]any) (string, error) {
			return "", errNope
		})
	}()

	_, err := Client(cConn, "dev-1", nil)
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("Client err = %v, want ErrRejected", err)
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err message %q missing reason", err.Error())
	}
}

func TestHandshakeProtocolMismatch(t *testing.T) {
	cConn, sConn := pair(t)

	go func() {
		_ = writeFrame(cConn, Hello{Version: 999, Type: TypeHello, DeviceID: "dev"})
		_, _ = readFrame(cConn) // drain server's REJECT so its write does not block
	}()

	_, _, err := Server(sConn, func(string, map[string]any) (string, error) {
		t.Fatal("auth must not be invoked on protocol mismatch")
		return "", nil
	})
	if !errors.Is(err, ErrProtocolVersion) {
		t.Fatalf("Server err = %v, want ErrProtocolVersion", err)
	}
}

func TestHandshakeUnexpectedType(t *testing.T) {
	cConn, sConn := pair(t)

	go func() {
		_ = writeFrame(cConn, Hello{Version: ProtoVersion, Type: "BOGUS", DeviceID: "dev"})
		_, _ = readFrame(cConn) // drain server's REJECT
	}()

	_, _, err := Server(sConn, func(string, map[string]any) (string, error) {
		t.Fatal("auth must not be invoked on bad type")
		return "", nil
	})
	if !errors.Is(err, ErrUnexpectedMessage) {
		t.Fatalf("Server err = %v, want ErrUnexpectedMessage", err)
	}
}

func TestReadFrameTooLarge(t *testing.T) {
	cConn, sConn := pair(t)

	go func() {
		var hdr [4]byte
		hdr[0] = 0xff
		hdr[1] = 0xff
		_, _ = cConn.Write(hdr[:])
		_ = cConn.Close()
	}()

	_, err := readFrame(sConn)
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("readFrame err = %v, want ErrFrameTooLarge", err)
	}
}

func TestReadFrameEOF(t *testing.T) {
	cConn, sConn := pair(t)
	_ = cConn.Close()

	_, err := readFrame(sConn)
	if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("readFrame err = %v", err)
	}
}

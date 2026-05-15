package jitsi

import (
	"encoding/base64"
	"testing"

	"github.com/zarazaex69/j"
)

func encodeForTest(t *testing.T, data []byte) string {
	t.Helper()
	return base64.StdEncoding.EncodeToString(data)
}

func makeBridgeMessage(class string, fields map[string]any) j.BridgeMessage {
	return j.BridgeMessage{
		Class:  class,
		Fields: fields,
	}
}

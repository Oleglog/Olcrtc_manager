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

func makeBridgeMessageFrom(class, from string, fields map[string]any) j.BridgeMessage {
	return j.BridgeMessage{
		Class:  class,
		From:   from,
		Fields: fields,
	}
}

func makeBridgeFrame(t *testing.T, payload []byte) string {
	t.Helper()
	framed := append([]byte{}, bridgeMagic[:]...)
	framed = append(framed, payload...)
	return base64.StdEncoding.EncodeToString(framed)
}

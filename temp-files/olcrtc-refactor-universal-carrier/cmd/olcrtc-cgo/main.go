// Package main exports a small c-shared olcRTC connectivity API for desktop clients.
package main

import "C"
import (
	"github.com/openlibrecommunity/olcrtc/mobile"
)

const errorResult = C.longlong(-1)

// Ping starts a short-lived olcRTC client, waits for its SOCKS listener,
// performs an HTTP ping through it, and returns latency in milliseconds.
// It returns -1 when arguments are invalid, startup fails, or the ping fails.
//
//export Ping
func Ping(
	carrierName *C.char,
	transportName *C.char,
	roomID *C.char,
	clientID *C.char,
	keyHex *C.char,
	socksPort C.longlong,
	timeoutMillis C.longlong,
	pingURL *C.char,
	vp8FPS C.longlong,
	vp8BatchSize C.longlong,
) C.longlong {
	result, err := mobile.Ping(
		goString(carrierName),
		goString(transportName),
		goString(roomID),
		goString(clientID),
		goString(keyHex),
		goInt(socksPort),
		goInt(timeoutMillis),
		goString(pingURL),
		goInt(vp8FPS),
		goInt(vp8BatchSize),
	)
	if err != nil {
		return errorResult
	}
	return C.longlong(result)
}

// Check starts a short-lived olcRTC client and returns elapsed milliseconds
// once the transport and local SOCKS listener are ready. It returns -1 on error.
//
//export Check
func Check(
	carrierName *C.char,
	transportName *C.char,
	roomID *C.char,
	clientID *C.char,
	keyHex *C.char,
	socksPort C.longlong,
	timeoutMillis C.longlong,
	vp8FPS C.longlong,
	vp8BatchSize C.longlong,
) C.longlong {
	result, err := mobile.Check(
		goString(carrierName),
		goString(transportName),
		goString(roomID),
		goString(clientID),
		goString(keyHex),
		goInt(socksPort),
		goInt(timeoutMillis),
		goInt(vp8FPS),
		goInt(vp8BatchSize),
	)
	if err != nil {
		return errorResult
	}
	return C.longlong(result)
}

func goString(value *C.char) string {
	if value == nil {
		return ""
	}
	return C.GoString(value)
}

func goInt(value C.longlong) int {
	const maxInt = int(^uint(0) >> 1)
	const minInt = -maxInt - 1
	if value > C.longlong(maxInt) {
		return maxInt
	}
	if value < C.longlong(minInt) {
		return minInt
	}
	return int(value)
}

func main() {}

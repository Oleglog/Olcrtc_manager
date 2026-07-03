// SPDX-License-Identifier: WTFPL

package protect

import (
	"context"
	"errors"
	"net"
	"reflect"
	"syscall"
	"testing"

	"github.com/pion/transport/v4"
)

func TestIsTunInterface(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"tun0":   true,
		"tun":    true,
		"ppp0":   true,
		"pptp0":  true,
		"wlan0":  false,
		"eth0":   false,
		"rmnet0": false,
		"lo":     false,
	}
	for name, want := range cases {
		if got := isTunInterface(name); got != want {
			t.Errorf("isTunInterface(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestProtectedNetInterfacesHideTun(t *testing.T) {
	t.Parallel()

	n, err := NewProtectedNet()
	if err != nil {
		t.Fatalf("NewProtectedNet: %v", err)
	}
	ifaces, err := n.Interfaces()
	if err != nil {
		t.Fatalf("Interfaces: %v", err)
	}
	for _, ifc := range ifaces {
		if isTunInterface(ifc.Name) {
			t.Errorf("Interfaces returned tunnel interface %q", ifc.Name)
		}
	}
}

func TestProtectedNetInterfaceByNameRejectsTun(t *testing.T) {
	t.Parallel()

	n, err := NewProtectedNet()
	if err != nil {
		t.Fatalf("NewProtectedNet: %v", err)
	}
	if _, err := n.InterfaceByName("tun0"); !errors.Is(err, transport.ErrInterfaceNotFound) {
		t.Errorf("InterfaceByName(tun0) error = %v, want %v", err, transport.ErrInterfaceNotFound)
	}
}

func TestProtectedNetCreateDialerProtectsAndChains(t *testing.T) {
	old := Protector
	t.Cleanup(func() { Protector = old })

	var protectorRan bool
	Protector = func(int) bool { protectorRan = true; return true }

	n, err := NewProtectedNet()
	if err != nil {
		t.Fatalf("NewProtectedNet: %v", err)
	}

	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		if c, aerr := ln.Accept(); aerr == nil {
			_ = c.Close()
		}
	}()

	var callerControlRan bool
	caller := &net.Dialer{
		Control: func(_, _ string, _ syscall.RawConn) error {
			callerControlRan = true
			return nil
		},
	}
	callerControl := caller.Control

	dialer := n.CreateDialer(caller)
	if reflect.ValueOf(caller.Control).Pointer() != reflect.ValueOf(callerControl).Pointer() {
		t.Error("CreateDialer mutated caller Dialer.Control")
	}

	conn, err := dialer.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial via CreateDialer: %v", err)
	}
	_ = conn.Close()
	if !protectorRan {
		t.Error("protector hook did not run")
	}
	if !callerControlRan {
		t.Error("caller Control hook did not run")
	}
}

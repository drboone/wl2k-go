// Copyright 2015 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

// Package netrom provides a net.Conn and net.Listener interfaces for NetROM.
//
// Supported TNCs
//
// This package currently implements interfaces for Linux' NetROM stack.
//
// Build tags
//
// The Linux AX.25 stack bindings are guarded by some custom build tags:
//
//    libax25 // Include support for Linux' AX.25 stack by linking against libax25.
//    static  // Link against static libraries only.
//

package netrom

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/la5nta/wl2k-go/transport"
)

const _NETWORK = "NetROM"

var DefaultDialer = &Dialer{Timeout: 45 * time.Second}

func init() {
	transport.RegisterDialer("netrom", DefaultDialer)
}

type addr interface {
	Address() Address // Callsign
}

type NetROMAddr struct{ addr }

func (a NetROMAddr) Network() string { return _NETWORK }
func (a NetROMAddr) String() string {
	var buf bytes.Buffer

	fmt.Fprint(&buf, a.Address())

	return buf.String()
}

type Address struct {
	Call string
	SSID uint8
}

type Conn struct {
	io.ReadWriteCloser
	localAddr  NetROMAddr
	remoteAddr NetROMAddr
}

func (c *Conn) LocalAddr() net.Addr {
	if !c.ok() {
		return nil
	}
	return c.localAddr
}

func (c *Conn) RemoteAddr() net.Addr {
	if !c.ok() {
		return nil
	}
	return c.remoteAddr
}

func (c *Conn) ok() bool { return c != nil }

func (c *Conn) SetDeadline(t time.Time) error {
	return errors.New(`SetDeadline not implemented`)
}

func (c *Conn) SetReadDeadline(t time.Time) error {
	return errors.New(`SetReadDeadline not implemented`)
}

func (c *Conn) SetWriteDeadline(t time.Time) error {
	return errors.New(`SetWriteDeadline not implemented`)
}

type Beacon interface {
	Now() error
	Every(d time.Duration) error

	LocalAddr() net.Addr
	RemoteAddr() net.Addr

	Message() string
}

type Dialer struct {
	Timeout time.Duration
}

func (d Dialer) DialURL(url *transport.URL) (net.Conn, error) {
	target := url.Target

	return DialNetROMTimeout(url.Host, url.User.Username(), target, d.Timeout)
}

func AddressFromString(str string) Address {
	parts := strings.Split(str, "-")
	addr := Address{Call: parts[0]}
	if len(parts) > 1 {
		ssid, err := strconv.ParseInt(parts[1], 10, 32)
		if err == nil && ssid >= 0 && ssid <= 255 {
			addr.SSID = uint8(ssid)
		}
	}
	return addr
}

func (a Address) String() string {
	if a.SSID > 0 {
		return fmt.Sprintf("%s-%d", a.Call, a.SSID)
	} else {
		return a.Call
	}
}

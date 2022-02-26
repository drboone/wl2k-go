// Copyright 2015 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

// +build libax25

package netrom

//#include <sys/socket.h>
import "C"

import (
	"fmt"
	"net"
	"syscall"
	"time"
	"unsafe"
)

func NewNetROMBeacon(nrPort, mycall, dest, message string) (Beacon, error) {
	if err := checkPort(nrPort); err != nil {
		return nil, err
	}

	localAddr := newNetROMAddr(mycall)
	if err := localAddr.setPort(nrPort); err != nil {
		return nil, err
	}
	remoteAddr := newNetROMAddr(dest)

	return &NetROMBeacon{localAddr, remoteAddr, message}, nil
}

type NetROMBeacon struct {
	localAddr  ax25Addr
	remoteAddr ax25Addr
	message    string
}

func (b *NetROMBeacon) Message() string      { return b.message }
func (b *NetROMBeacon) LocalAddr() net.Addr  { return NetROMAddr{b.localAddr} }
func (b *NetROMBeacon) RemoteAddr() net.Addr { return NetROMAddr{b.remoteAddr} }

func (b *NetROMBeacon) Every(d time.Duration) error {
	for {
		if err := b.Now(); err != nil {
			return err
		}
		time.Sleep(d)
	}
}

func (b *NetROMBeacon) Now() error {
	// Create file descriptor
	//REVIEW: Should we keep it for next beacon?
	var socket fd
	if f, err := syscall.Socket(syscall.AF_NETROM, syscall.SOCK_DGRAM, 0); err != nil {
		return err
	} else {
		socket = fd(f)
	}
	defer socket.close()

	if err := socket.bind(b.localAddr); err != nil {
		return fmt.Errorf("bind: %s", err)
	}

	msg := C.CString(b.message)
	_, err := C.sendto(
		C.int(socket),
		unsafe.Pointer(msg),
		C.size_t(len(b.message)),
		0,
		(*C.struct_sockaddr)(unsafe.Pointer(&b.remoteAddr)),
		C.socklen_t(unsafe.Sizeof(b.remoteAddr)),
	)

	return err
}

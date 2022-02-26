// Copyright 2015 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

// +build libax25

package netrom

/*
#include <sys/socket.h>
#include <netax25/ax25.h>
#include <netax25/axlib.h>
#include <netax25/axconfig.h>
#include <netax25/nrconfig.h>
#include <netrom/netrom.h>
#include <fcntl.h>
*/
import "C"

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
	"time"
	"unsafe"
)

type ax25Addr C.struct_full_sockaddr_ax25

var numNRPorts int

// bug(martinhpedersen): The AX.25 stack does not support SOCK_STREAM, so any write to the connection
// that is larger than maximum packet length will fail. The b2f impl. requires 125 bytes long packets.
var ErrMessageTooLong = errors.New("Write: Message too long. Consider increasing maximum packet length to >= 125.")
var ErrPortNotExist = errors.New("No such NR port found")

type fd uintptr

type netromListener struct {
	sock      fd
	localAddr NetROMAddr
	close     chan struct{}
}

func portExists(port string) bool { return C.nr_config_get_dev(C.CString(port)) != nil }

func loadPorts() (int, error) {
	if numNRPorts > 0 {
		return numNRPorts, nil
	}

	n, err := C.nr_config_load_ports()
	if err != nil {
		return int(n), err
	} else if n == 0 {
		return 0, fmt.Errorf("No NetROM ports configured")
	}

	numNRPorts = int(n)
	return numNRPorts, err
}

func checkPort(nrPort string) error {
	if nrPort == "" {
		return errors.New("Invalid empty nrPort")
	}
	if _, err := loadPorts(); err != nil {
		return err
	}
	if !portExists(nrPort) {
		return ErrPortNotExist
	}
	return nil
}

// Addr returns the listener's network address, an NetROMAddr.
func (ln netromListener) Addr() net.Addr { return ln.localAddr }

// Close stops listening on the NetROM port. Already Accepted connections are not closed.
func (ln netromListener) Close() error { close(ln.close); return ln.sock.close() }

// Accept waits for the next call and returns a generic Conn.
//
// See net.Listener for more information.
func (ln netromListener) Accept() (net.Conn, error) {
	err := ln.sock.waitRead(ln.close)
	if err != nil {
		return nil, err
	}

	nfd, addr, err := ln.sock.accept()
	if err != nil {
		return nil, err
	}

	conn := &Conn{
		localAddr:       ln.localAddr,
		remoteAddr:      NetROMAddr{addr},
		ReadWriteCloser: os.NewFile(uintptr(nfd), ""),
	}

	return conn, nil
}

// ListenNetROM announces on the local port nrPort using mycall as the local address.
//
// An error will be returned if nrPort is empty.
func ListenNetROM(nrPort, mycall string) (net.Listener, error) {
	if err := checkPort(nrPort); err != nil {
		return nil, err
	}

	// Setup local address (via callsign of supplied nrPort)
	localAddr := newNetROMAddr(mycall)
	if err := localAddr.setPort(nrPort); err != nil {
		return nil, err
	}

	// Create file descriptor
	var socket fd
	if f, err := syscall.Socket(syscall.AF_NETROM, syscall.SOCK_SEQPACKET, 0); err != nil {
		return nil, err
	} else {
		socket = fd(f)
	}

	if err := socket.bind(localAddr); err != nil {
		return nil, err
	}
	if err := syscall.Listen(int(socket), syscall.SOMAXCONN); err != nil {
		return nil, err
	}

	return netromListener{
		sock:      fd(socket),
		localAddr: NetROMAddr{localAddr},
		close:     make(chan struct{}),
	}, nil
}

// DialNetROMTimeout acts like DialNetROM but takes a timeout.
func DialNetROMTimeout(nrPort, mycall, targetcall string, timeout time.Duration) (*Conn, error) {
	if err := checkPort(nrPort); err != nil {
		return nil, err
	}

	// Setup local address (via callsign of supplied nrPort)
	localAddr := newNetROMAddr(mycall)
	if err := localAddr.setPort(nrPort); err != nil {
		return nil, err
	}
	remoteAddr := newNetROMAddr(targetcall)

	// Create file descriptor
	var socket fd
	if f, err := syscall.Socket(syscall.AF_NETROM, syscall.SOCK_SEQPACKET, 0); err != nil {
		return nil, err
	} else {
		socket = fd(f)
	}
	// Bind
	if err := socket.bind(localAddr); err != nil {
		return nil, err
	}

	// Connect
	err := socket.connectTimeout(remoteAddr, timeout)
	if err != nil {
		socket.close()
		return nil, err
	}

	return &Conn{
		ReadWriteCloser: os.NewFile(uintptr(socket), nrPort),
		localAddr:       NetROMAddr{localAddr},
		remoteAddr:      NetROMAddr{remoteAddr},
	}, nil
}

func (c *Conn) Close() error {
	if !c.ok() {
		return syscall.EINVAL
	}

	return c.ReadWriteCloser.Close()
}

func (c *Conn) Write(p []byte) (n int, err error) {
	if !c.ok() {
		return 0, syscall.EINVAL
	}

	n, err = c.ReadWriteCloser.Write(p)
	perr, ok := err.(*os.PathError)
	if !ok {
		return
	}

	switch perr.Err.Error() {
	case "message too long":
		return n, ErrMessageTooLong
	default:
		return
	}
}

func (c *Conn) Read(p []byte) (n int, err error) {
	if !c.ok() {
		return 0, syscall.EINVAL
	}

	n, err = c.ReadWriteCloser.Read(p)
	perr, ok := err.(*os.PathError)
	if !ok {
		return
	}

	//TODO: These errors should not be checked using string comparison!
	// The weird error handling here is needed because of how the *os.File treats
	// the underlying fd. This should be fixed the same way as net.FileConn does.
	switch perr.Err.Error() {
	case "transport endpoint is not connected": // We get this error when the remote hangs up
		return n, io.EOF
	default:
		return
	}
}

// DialNetROM connects to the remote station targetcall using the named nrPort and mycall.
//
// An error will be returned if nrPort is empty.
func DialNetROM(nrPort, mycall, targetcall string) (*Conn, error) {
	return DialNetROMTimeout(nrPort, mycall, targetcall, 0)
}

func (sock fd) connectTimeout(addr ax25Addr, timeout time.Duration) (err error) {
	if timeout == 0 {
		return sock.connect(addr)
	}
	if err = syscall.SetNonblock(int(sock), true); err != nil {
		return err
	}

	err = sock.connect(addr)
	if err == nil {
		return nil // Connected
	} else if err != syscall.EINPROGRESS {
		return err
	}

	fdset := new(syscall.FdSet)
	maxFd := fdSet(fdset, int(sock))

	// Wait or timeout
	var n int
	var tv syscall.Timeval
	for {
		tv = syscall.NsecToTimeval(int64(timeout))
		n, err = syscall.Select(maxFd+1, nil, fdset, nil, &tv)
		if n < 0 && err != syscall.EINTR {
			sock.close()
			return err
		} else if n > 0 {
			// Verify that connection is OK
			nerr, err := syscall.GetsockoptInt(int(sock), syscall.SOL_SOCKET, syscall.SO_ERROR)
			if err != nil {
				sock.close()
				return err
			}
			err = syscall.Errno(nerr)
			if nerr != 0 && err != syscall.EINPROGRESS && err != syscall.EALREADY && err != syscall.EINTR {
				sock.close()
				return err
			} else {
				break // Connected
			}
		} else {
			sock.close()
			return fmt.Errorf("Dial timeout")
		}
	}

	syscall.SetNonblock(int(sock), false)
	return
}

// waitRead blocks until the socket is ready for read or the call is canceled
//
// The error syscall.EINVAL is returned if the cancel channel is closed, indicating
// that the socket is being closed by another thread.
func (sock fd) waitRead(cancel <-chan struct{}) error {
	pr, pw, err := os.Pipe()
	if err != nil {
		return err
	}

	done := make(chan struct{})
	go func() {
		select {
		case <-cancel:
			pw.Write([]byte("\n"))
		case <-done:
			return
		}
	}()
	defer func() { close(done); pw.Close() }()

	fdset := new(syscall.FdSet)
	maxFd := fdSet(fdset, int(sock), int(pr.Fd()))

	syscall.SetNonblock(int(sock), true)
	defer func() { syscall.SetNonblock(int(sock), false) }()

	var n int
	for {
		n, err = syscall.Select(maxFd+1, fdset, nil, nil, nil)
		if n < 0 || err != nil {
			return err
		}

		if fdIsSet(fdset, int(sock)) {
			break // sock is ready for read
		} else {
			return syscall.EINVAL
		}
	}
	return nil
}

func (sock fd) close() error {
	return syscall.Close(int(sock))
}

func (sock fd) accept() (nfd fd, addr ax25Addr, err error) {
	addrLen := C.socklen_t(unsafe.Sizeof(addr))
	n, err := C.accept(
		C.int(sock),
		(*C.struct_sockaddr)(unsafe.Pointer(&addr)),
		&addrLen)

	if addrLen != C.socklen_t(unsafe.Sizeof(addr)) {
		panic("unexpected socklet_t")
	}

	return fd(n), addr, err
}

func (sock fd) connect(addr ax25Addr) (err error) {
	_, err = C.connect(
		C.int(sock),
		(*C.struct_sockaddr)(unsafe.Pointer(&addr)),
		C.socklen_t(unsafe.Sizeof(addr)))

	return
}

func (sock fd) bind(addr ax25Addr) (err error) {
	_, err = C.bind(
		C.int(sock),
		(*C.struct_sockaddr)(unsafe.Pointer(&addr)),
		C.socklen_t(unsafe.Sizeof(addr)))

	return
}

type ax25_address *C.ax25_address

func (a ax25Addr) Address() Address {
	return AddressFromString(
		C.GoString(C.ax25_ntoa(a.ax25_address())),
	)
}

func (a *ax25Addr) ax25_address() ax25_address {
	return (*C.ax25_address)(unsafe.Pointer(&a.fsa_ax25.sax25_call.ax25_call))
}

func (a *ax25Addr) setPort(port string) (err error) {
	C.ax25_aton(
		C.nr_config_get_addr(C.CString(port)),
		(*C.struct_full_sockaddr_ax25)(unsafe.Pointer(&a)),
	)
	return
}

func newNetROMAddr(address string) ax25Addr {
	var addr C.struct_full_sockaddr_ax25

	if C.ax25_aton(C.CString(address), &addr) < 0 {
		panic("ax25_aton")
	}
	addr.fsa_ax25.sax25_family = syscall.AF_NETROM

	return ax25Addr(addr)
}

func fdSet(p *syscall.FdSet, fd ...int) (max int) {
	// Shamelessly stolen from src/pkg/exp/inotify/inotify_linux.go:
	//
	// Create fdSet, taking into consideration that
	// 64-bit OS uses Bits: [16]int64, while 32-bit OS uses Bits: [32]int32.
	// This only support File Descriptors up to 1024
	//
	fElemSize := 32 * 32 / len(p.Bits)

	for _, i := range fd {
		if i > 1024 {
			panic(fmt.Errorf("fdSet: File Descriptor >= 1024: %v", i))
		}
		if i > max {
			max = i
		}
		p.Bits[i/fElemSize] |= 1 << uint(i%fElemSize)
	}
	return max
}

func fdIsSet(p *syscall.FdSet, i int) bool {
	fElemSize := 32 * 32 / len(p.Bits)
	return p.Bits[i/fElemSize]&(1<<uint(i%fElemSize)) != 0
}

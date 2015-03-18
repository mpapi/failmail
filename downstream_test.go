package main

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/textproto"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

var testLogger = log.New(ioutil.Discard, "", log.LstdFlags)

type BadClient struct{}

func (b BadClient) Read(p []byte) (int, error) {
	return 0, fmt.Errorf("bad read from bad client")
}

func (b BadClient) Write(p []byte) (int, error) {
	return len(p), nil
}

func (b BadClient) Close() error {
	return nil
}

type BadServerSocket struct {
}

func (b *BadServerSocket) Accept() (net.Conn, error) {
	return nil, fmt.Errorf("bad accept")
}

func (b *BadServerSocket) Close() error {
	return fmt.Errorf("bad close")
}

func (b *BadServerSocket) Addr() net.Addr {
	return nil
}

func (b *BadServerSocket) Fd() (uintptr, error) {
	return 0, nil
}

func (b *BadServerSocket) String() string {
	return "bad"
}

type MockServerSocket struct {
	conns  []net.Conn
	closes chan bool
}

func NewMockSocket() (*MockServerSocket, net.Conn) {
	server, client := net.Pipe()
	socket := &MockServerSocket{[]net.Conn{server}, make(chan bool, 0)}

	return socket, client
}

func (m *MockServerSocket) Accept() (net.Conn, error) {
	if len(m.conns) == 0 {
		<-m.closes
		return nil, fmt.Errorf("no more connections")
	}
	conn := m.conns[0]
	m.conns = append(m.conns[:0], m.conns[1:]...)
	return conn, nil
}

func (m *MockServerSocket) Addr() net.Addr {
	return nil
}

func (m *MockServerSocket) Close() error {
	m.closes <- true
	return nil
}

func (m *MockServerSocket) Fd() (uintptr, error) {
	return 99, nil
}

func (m *MockServerSocket) String() string {
	return "mock"
}

func TestListener(t *testing.T) {
	socket, client := NewMockSocket()

	listener := &Listener{Socket: socket}
	shutdown := make(chan TerminationRequest, 0)
	received := make(chan *StorageRequest, 1)

	go connectAndShutdown(t, textproto.NewConn(client), shutdown, GracefulShutdown)

	listener.Listen(received, shutdown, 100*time.Millisecond)
}

func TestListenerWithFileSocket(t *testing.T) {
	tcpSocket, err := NewTCPServerSocket("localhost:10010")
	if err != nil {
		t.Fatalf("failed to create TCP socket: %s", err)
	}

	fd, err := tcpSocket.Fd()
	if err != nil {
		t.Fatalf("failed to get file descriptor from TCP socket: %s", err)
	}

	socket, err := NewFileServerSocket(fd)
	if err != nil {
		t.Fatalf("failed to create socket from fd: %s", err)
	}
	defer socket.Close()

	listener := &Listener{Socket: socket, Debug: true}
	shutdown := make(chan TerminationRequest, 0)
	received := make(chan *StorageRequest, 1)

	go dialAndShutdown(t, "localhost:10010", shutdown, GracefulShutdown)

	listener.Listen(received, shutdown, 100*time.Millisecond)
}

func TestListenerReload(t *testing.T) {
	socket, err := NewTCPServerSocket("localhost:10020")
	if err != nil {
		t.Fatalf("failed to create socket")
	}
	defer socket.Close()

	listener := &Listener{Socket: socket}
	shutdown := make(chan TerminationRequest, 0)
	received := make(chan *StorageRequest, 1)

	go dialAndShutdown(t, "localhost:10020", shutdown, Reload)

	if fd, err := listener.Listen(received, shutdown, 1*time.Second); err != nil {
		t.Fatalf("unexpected error returned from Listen(): %s", err)
	} else if fd <= 0 {
		t.Fatalf("unexpected file descriptor returned from Listen(): %d", fd)
	}
}

func TestListenerWithMessage(t *testing.T) {
	socket, client := NewMockSocket()

	listener := &Listener{Socket: socket}
	shutdown := make(chan TerminationRequest, 0)
	received := make(chan *StorageRequest, 1)

	go func() {
		req := <-received
		req.StorageErrors <- nil
	}()

	go func() {
		conn := textproto.NewConn(client)

		if _, _, err := conn.ReadCodeLine(220); err != nil {
			t.Errorf("unexpected response from server: %s", err)
		}

		sendAndExpect(conn, t, "HELO localhost", 250)
		sendAndExpect(conn, t, "MAIL FROM:<test@localhost>", 250)
		sendAndExpect(conn, t, "RCPT TO:<test@localhost>", 250)
		sendAndExpect(conn, t, "DATA", 354)
		sendAndExpect(conn, t, "Subject: test\r\n\r\nbody\r\n.", 250)
		sendAndExpect(conn, t, "QUIT", 221)

		if err := conn.Close(); err != nil {
			t.Errorf("failed to close listener: %s", err)
		}

		shutdown <- GracefulShutdown
	}()

	listener.Listen(received, shutdown, 100*time.Millisecond)
}

func TestListenerWithBadClient(t *testing.T) {
	buf := new(bytes.Buffer)
	log.SetOutput(buf)
	defer log.SetOutput(os.Stderr)

	l := &Listener{}
	received := make(chan *StorageRequest, 1)
	l.handleConnection(BadClient{}, received)
	if msg := string(buf.Bytes()); !strings.Contains(msg, "bad read from bad client") {
		t.Errorf("bad client didn't trigger failure in handleConnection(): %#v", msg)
	}
}

func TestListenerWithBadServer(t *testing.T) {
	buf := new(bytes.Buffer)
	log.SetOutput(buf)
	defer log.SetOutput(os.Stderr)

	socket := new(BadServerSocket)
	l := &Listener{Socket: socket}

	received := make(chan *StorageRequest, 1)
	l.Listen(received, make(chan TerminationRequest, 0), 100*time.Millisecond)

	if msg := string(buf.Bytes()); !strings.Contains(msg, "bad accept") {
		t.Errorf("bad socket Accept() didn't trigger failure in Listen(): %#v", msg)
	}
}

func TestListenerWithAuth(t *testing.T) {
	socket, client := NewMockSocket()

	auth := &SingleUserPlainAuth{"test", "test", true}
	listener := &Listener{Socket: socket, Auth: auth}
	shutdown := make(chan TerminationRequest, 0)
	received := make(chan *StorageRequest, 1)

	go func() {
		conn := textproto.NewConn(client)

		_, _, err := conn.ReadCodeLine(220)
		if err != nil {
			t.Errorf("unexpected response from server: %s", err)
		}

		sendAndExpect(conn, t, "HELO localhost", 250)
		sendAndExpect(conn, t, "AUTH PLAIN dGVzdAB0ZXN0AHRlc3Q=", 235)
		sendAndExpect(conn, t, "QUIT", 221)

		err = conn.Close()
		if err != nil {
			t.Errorf("failed to close listener: %s", err)
		}

		shutdown <- GracefulShutdown
	}()

	listener.Listen(received, shutdown, 100*time.Millisecond)
}

func TestListenerWithPartialAuth(t *testing.T) {
	socket, client := NewMockSocket()

	auth := &SingleUserPlainAuth{"test", "test", true}
	listener := &Listener{Socket: socket, Auth: auth}
	shutdown := make(chan TerminationRequest, 0)
	received := make(chan *StorageRequest, 1)

	go func() {
		conn := textproto.NewConn(client)

		_, _, err := conn.ReadCodeLine(220)
		if err != nil {
			t.Errorf("unexpected response from server: %s", err)
		}

		sendAndExpect(conn, t, "HELO localhost", 250)
		sendAndExpect(conn, t, "AUTH PLAIN", 334)
		sendAndExpect(conn, t, "dGVzdAB0ZXN0AHRlc3Q=", 235)
		sendAndExpect(conn, t, "QUIT", 221)

		err = conn.Close()
		if err != nil {
			t.Errorf("failed to close listener: %s", err)
		}

		shutdown <- GracefulShutdown
	}()

	listener.Listen(received, shutdown, 100*time.Millisecond)
}

func TestListenerWithTLS(t *testing.T) {
	socket, err := NewTCPServerSocket("localhost:10030")
	if err != nil {
		t.Fatalf("failed to create socket")
	}
	defer socket.Close()

	certs := buildCerts()
	if len(certs) == 0 {
		t.Fatalf("failed to read certificates for TLS test")
	}
	listener := &Listener{Socket: socket, Security: TLS_PRE_STARTTLS, TLSConfig: &tls.Config{Certificates: certs}}
	shutdown := make(chan TerminationRequest, 0)
	received := make(chan *StorageRequest, 1)

	go func() {
		rawConn, err := net.Dial("tcp", "localhost:10030")
		if err != nil {
			t.Fatalf("failed to connect to listener: %s", err)
		}

		conn := textproto.NewConn(rawConn)

		_, _, err = conn.ReadCodeLine(220)
		if err != nil {
			t.Errorf("unexpected response from server: %s", err)
		}

		sendAndExpect(conn, t, "HELO localhost", 250)
		sendAndExpect(conn, t, "STARTTLS", 220)
		conn = textproto.NewConn(tls.Client(rawConn, &tls.Config{InsecureSkipVerify: true}))
		sendAndExpect(conn, t, "QUIT", 221)

		if err := conn.Close(); err != nil {
			t.Errorf("failed to close listener: %s", err)
		}

		shutdown <- GracefulShutdown
	}()

	listener.Listen(received, shutdown, 100*time.Millisecond)
}

func sendAndExpect(conn *textproto.Conn, t *testing.T, line string, code int) {
	err := conn.PrintfLine(line)
	if err != nil {
		t.Errorf("unexpected error writing to server: %s", err)
	}

	_, _, err = conn.ReadCodeLine(code)
	if err != nil {
		t.Errorf("unexpected response from server: %s", err)
	}
}

func TestWaitWithTimeoutNoTimeout(t *testing.T) {
	wg := new(sync.WaitGroup)
	wg.Add(1)
	if noTimeout := WaitWithTimeout(wg, 10*time.Millisecond); noTimeout {
		t.Errorf("expected a timeout from WaitWithTimeout")
	}
}

func TestWaitWithTimeout(t *testing.T) {
	wg := new(sync.WaitGroup)
	if noTimeout := WaitWithTimeout(wg, 10*time.Millisecond); !noTimeout {
		t.Errorf("expected no timeout from WaitWithTimeout")
	}
}

func buildCerts() []tls.Certificate {
	pemCert := `-----BEGIN CERTIFICATE-----
MIIB0zCCAX2gAwIBAgIJAN3/7+49TYhaMA0GCSqGSIb3DQEBCwUAMEUxCzAJBgNV
BAYTAkFVMRMwEQYDVQQIDApTb21lLVN0YXRlMSEwHwYDVQQKDBhJbnRlcm5ldCBX
aWRnaXRzIFB0eSBMdGQwHhcNMTQwOTI4MTMyODMzWhcNMTQxMDI4MTMyODMzWjBF
MQswCQYDVQQGEwJBVTETMBEGA1UECAwKU29tZS1TdGF0ZTEhMB8GA1UECgwYSW50
ZXJuZXQgV2lkZ2l0cyBQdHkgTHRkMFwwDQYJKoZIhvcNAQEBBQADSwAwSAJBAMQB
p1QnWVSC8kkc1HViRMUR7GIBuE4dlb/8rJ/WLaD0lT1t1eNWYZNrbWJ3vSRVSNv+
1CCKj1rDyjfSfX8O430CAwEAAaNQME4wHQYDVR0OBBYEFJA4xJvhsRGC/xlBTlMS
XCf8McIMMB8GA1UdIwQYMBaAFJA4xJvhsRGC/xlBTlMSXCf8McIMMAwGA1UdEwQF
MAMBAf8wDQYJKoZIhvcNAQELBQADQQCm1i+WaR/2y0jBsHBoX5BkqqAemZeGXtxM
P1Vcabz8ZWDEPjAliWBzQuWO15cDMiLXxW2QekVPTO1b4ZiB1Mvp
-----END CERTIFICATE-----`
	pemKey := `-----BEGIN RSA PRIVATE KEY-----
MIIBOgIBAAJBAMQBp1QnWVSC8kkc1HViRMUR7GIBuE4dlb/8rJ/WLaD0lT1t1eNW
YZNrbWJ3vSRVSNv+1CCKj1rDyjfSfX8O430CAwEAAQJAIdETOH6td9o7yQdzVGlG
6iVEfkhDrx6FlqEWe2EtcCZVR3nyl6d2HbRy9kyvwECQlPqpHZRVzqq1Q8gElAuz
1QIhAONmXF36or6hrzr8ov4kOQ24QuyyE5l0aOo/YFMteh9fAiEA3KiDdqZuRSmC
Zv+GaFr1+MRXt1ZAXV5RL6e5lsodVqMCIQDTCUsNeK4ShpDOCGnnu4wrXGbXrcgc
sPkw89IcP2dHtwIgduZOwHZ54Ma3P6zczgqFlCCoa2AMmsMh2B32wSvzlyUCIDnu
3kB1gcsw+gLW70mbZxw+tAx6a7kBDNz+VCLW6RDT
-----END RSA PRIVATE KEY-----`
	cert, err := tls.X509KeyPair([]byte(pemCert), []byte(pemKey))
	if err != nil {
		return []tls.Certificate{}
	}
	return []tls.Certificate{cert}
}

func dialAndShutdown(t *testing.T, addr string, shutdown chan<- TerminationRequest, req TerminationRequest) {
	conn, err := textproto.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect to listener: %s", err)
	}
	connectAndShutdown(t, conn, shutdown, req)
}

func connectAndShutdown(t *testing.T, conn *textproto.Conn, shutdown chan<- TerminationRequest, req TerminationRequest) {
	if _, _, err := conn.ReadCodeLine(220); err != nil {
		t.Errorf("unexpected response from server: %s", err)
	}

	if err := conn.PrintfLine("QUIT"); err != nil {
		t.Errorf("unexpected error writing to server: %s", err)
	}

	if _, _, err := conn.ReadCodeLine(221); err != nil {
		t.Errorf("unexpected response from server: %s", err)
	}

	if err := conn.Close(); err != nil {
		t.Errorf("failed to close listener: %s", err)
	}

	shutdown <- req
}

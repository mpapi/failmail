package main

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/textproto"
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
	return 0, nil
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

func TestListener(t *testing.T) {
	socket, err := NewTCPServerSocket("localhost:40025")
	if err != nil {
		t.Fatalf("failed to create socket")
	}
	listener := &Listener{Socket: socket, connLimit: 1}
	received := make(chan *StorageRequest, 1)
	done := make(chan bool, 0)

	go func() {
		conn, err := textproto.Dial("tcp", "localhost:40025")
		if err != nil {
			t.Fatalf("failed to connect to listener: %s", err)
		}

		_, _, err = conn.ReadCodeLine(220)
		if err != nil {
			t.Errorf("unexpected response from server: %s", err)
		}

		err = conn.PrintfLine("QUIT")
		if err != nil {
			t.Errorf("unexpected error writing to server: %s", err)
		}

		_, _, err = conn.ReadCodeLine(221)
		if err != nil {
			t.Errorf("unexpected response from server: %s", err)
		}

		err = conn.Close()
		if err != nil {
			t.Errorf("failed to close listener: %s", err)
		}

		done <- true
	}()

	listener.Listen(received, make(chan TerminationRequest, 0), 100*time.Millisecond)
	<-done
}

func TestListenerWithFileSocket(t *testing.T) {
	tcpSocket, err := NewTCPServerSocket("localhost:40040")
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

	listener := &Listener{Socket: socket, connLimit: 1}
	received := make(chan *StorageRequest, 1)
	done := make(chan bool, 0)

	go func() {
		conn, err := textproto.Dial("tcp", "localhost:40040")
		if err != nil {
			t.Fatalf("failed to connect to listener: %s", err)
		}

		_, _, err = conn.ReadCodeLine(220)
		if err != nil {
			t.Errorf("unexpected response from server: %s", err)
		}

		err = conn.PrintfLine("QUIT")
		if err != nil {
			t.Errorf("unexpected error writing to server: %s", err)
		}

		_, _, err = conn.ReadCodeLine(221)
		if err != nil {
			t.Errorf("unexpected response from server: %s", err)
		}

		err = conn.Close()
		if err != nil {
			t.Errorf("failed to close listener: %s", err)
		}

		done <- true
	}()

	listener.Listen(received, make(chan TerminationRequest, 0), 100*time.Millisecond)
	<-done
}

func TestListenerShutdown(t *testing.T) {
	socket, err := NewTCPServerSocket("localhost:40041")
	if err != nil {
		t.Fatalf("failed to create TCP socket: %s", err)
	}

	listener := &Listener{Socket: socket}
	received := make(chan *StorageRequest, 1)
	shutdown := make(chan TerminationRequest, 0)
	done := make(chan bool, 0)

	go func() {
		conn, err := textproto.Dial("tcp", "localhost:40041")
		if err != nil {
			t.Fatalf("failed to connect to listener: %s", err)
		}

		_, _, err = conn.ReadCodeLine(220)
		if err != nil {
			t.Errorf("unexpected response from server: %s", err)
		}

		shutdown <- Reload

		err = conn.PrintfLine("QUIT")
		if err != nil {
			t.Errorf("unexpected error writing to server: %s", err)
		}

		_, _, err = conn.ReadCodeLine(221)
		if err != nil {
			t.Errorf("unexpected response from server: %s", err)
		}

		err = conn.Close()
		if err != nil {
			t.Errorf("failed to close listener: %s", err)
		}

		done <- true
	}()

	if fd, err := listener.Listen(received, shutdown, 1*time.Second); err != nil {
		t.Fatalf("unexpected error returned from Listen(): %s", err)
	} else if fd <= 0 {
		t.Fatalf("unexpected file descriptor returned from Listen(): %d", fd)
	}

	<-done
}

func TestListenerWithMessage(t *testing.T) {
	socket, err := NewTCPServerSocket("localhost:40026")
	if err != nil {
		t.Fatalf("failed to create socket")
	}
	listener := &Listener{Socket: socket, connLimit: 1}
	received := make(chan *StorageRequest, 1)
	done := make(chan bool, 0)

	go func() {
		req := <-received
		req.StorageErrors <- nil
	}()

	go func() {
		conn, err := textproto.Dial("tcp", "localhost:40026")
		if err != nil {
			t.Fatalf("failed to connect to listener: %s", err)
		}

		_, _, err = conn.ReadCodeLine(220)
		if err != nil {
			t.Errorf("unexpected response from server: %s", err)
		}

		sendAndExpect(conn, t, "HELO localhost", 250)
		sendAndExpect(conn, t, "MAIL FROM:<test@localhost>", 250)
		sendAndExpect(conn, t, "RCPT TO:<test@localhost>", 250)
		sendAndExpect(conn, t, "DATA", 354)
		sendAndExpect(conn, t, "Subject: test\r\n\r\nbody\r\n.", 250)
		sendAndExpect(conn, t, "QUIT", 221)

		err = conn.Close()
		if err != nil {
			t.Errorf("failed to close listener: %s", err)
		}

		done <- true
	}()

	listener.Listen(received, make(chan TerminationRequest, 0), 100*time.Millisecond)
	<-done
}

func TestListenerWithBadClient(t *testing.T) {
	buf := new(bytes.Buffer)
	socket, err := NewTCPServerSocket("localhost:40027")
	if err != nil {
		t.Fatalf("failed to create socket")
	}
	l := &Listener{socket, nil, nil, false, 0, 0}
	received := make(chan *StorageRequest, 1)
	l.handleConnection(BadClient{}, received)
	if msg := string(buf.Bytes()); strings.HasSuffix(msg, "bad read from bad client") {
		t.Errorf("bad client didn't trigger failure in handleConnection(): %#v", msg)
	}
}

func TestListenerWithBadServer(t *testing.T) {
	buf := new(bytes.Buffer)
	socket := new(BadServerSocket)
	l := &Listener{socket, nil, nil, false, 0, 0}

	received := make(chan *StorageRequest, 1)
	done := make(chan bool, 1)
	go func() {
		l.Listen(received, make(chan TerminationRequest, 0), 1*time.Millisecond)
		done <- true
	}()

	select {
	case <-time.Tick(1 * time.Second):
		t.Errorf("timed out")
	case <-done:
	}

	if msg := string(buf.Bytes()); strings.HasSuffix(msg, "error accepting connection") {
		t.Errorf("bad socket Accept() didn't trigger failure in Listen(): %#v", msg)
	}
}

func TestListenerWithAuth(t *testing.T) {
	socket, err := NewTCPServerSocket("localhost:40028")
	if err != nil {
		t.Fatalf("failed to create socket")
	}
	auth := &SingleUserPlainAuth{Username: "test", Password: "test"}
	listener := &Listener{Socket: socket, connLimit: 1, Auth: auth}
	received := make(chan *StorageRequest, 1)
	done := make(chan bool, 0)

	go func() {
		conn, err := textproto.Dial("tcp", "localhost:40028")
		if err != nil {
			t.Fatalf("failed to connect to listener: %s", err)
		}

		_, _, err = conn.ReadCodeLine(220)
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

		done <- true
	}()

	listener.Listen(received, make(chan TerminationRequest, 0), 100*time.Millisecond)
	<-done
}

func TestListenerWithPartialAuth(t *testing.T) {
	socket, err := NewTCPServerSocket("localhost:40029")
	if err != nil {
		t.Fatalf("failed to create socket")
	}
	auth := &SingleUserPlainAuth{Username: "test", Password: "test"}
	listener := &Listener{Socket: socket, connLimit: 1, Auth: auth}
	received := make(chan *StorageRequest, 1)
	done := make(chan bool, 0)

	go func() {
		conn, err := textproto.Dial("tcp", "localhost:40029")
		if err != nil {
			t.Fatalf("failed to connect to listener: %s", err)
		}

		_, _, err = conn.ReadCodeLine(220)
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

		done <- true
	}()

	listener.Listen(received, make(chan TerminationRequest, 0), 100*time.Millisecond)
	<-done
}

func TestListenerWithTLS(t *testing.T) {
	socket, err := NewTCPServerSocket("localhost:40030")
	if err != nil {
		t.Fatalf("failed to create socket")
	}
	certs := buildCerts()
	if len(certs) == 0 {
		t.Fatalf("failed to read certificates for TLS test")
	}
	listener := &Listener{Socket: socket, connLimit: 1, TLSConfig: &tls.Config{Certificates: certs}}
	received := make(chan *StorageRequest, 1)
	done := make(chan bool, 0)

	go func() {
		rawConn, err := net.Dial("tcp", "localhost:40030")
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

		err = conn.Close()
		if err != nil {
			t.Errorf("failed to close listener: %s", err)
		}

		done <- true
	}()

	listener.Listen(received, make(chan TerminationRequest, 0), 100*time.Millisecond)
	<-done
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

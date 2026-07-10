package test

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"net"
	"testing"
	"time"

	snell "github.com/sagernet/sing-snell"
	"github.com/sagernet/sing-snell/snellv4"
	"github.com/sagernet/sing-snell/snellv5"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	"github.com/stretchr/testify/require"
)

func TestV5ObfsTLSLoopback(t *testing.T) {
	service, err := snellv5.NewService(snellv5.ServiceOptions{
		PSK:      []byte(testPSK),
		ObfsMode: snell.ObfsModeTLS,
		Handler:  localEchoHandler{},
	})
	require.NoError(t, err)
	serviceAddress := startLocalSnellService(t, service)
	client, err := snellv4.NewClient(snellv4.ClientOptions{
		PSK:      []byte(testPSK),
		ObfsMode: snell.ObfsModeTLS,
		ObfsHost: "obfs.example",
	})
	require.NoError(t, err)
	normalScenario{address: serviceAddress, client: client}.HalfCloseEcho(t, "v5-tls-tcp")

	var proxy countingTCPProxy
	proxy.Start(t, serviceAddress)
	reuseClient, err := snellv4.NewClient(snellv4.ClientOptions{
		PSK:      []byte(testPSK),
		Reuse:    true,
		ObfsMode: snell.ObfsModeTLS,
		ObfsHost: "obfs.example",
		Dialer:   N.SystemDialer,
		Server:   M.ParseSocksaddr(proxy.address),
	})
	require.NoError(t, err)
	defer reuseClient.Close()
	reuseScenario{client: reuseClient, destination: M.ParseSocksaddrHostPort("127.0.0.1", 443)}.RoundTrip(t, "v5-tls-reuse-1")
	reuseScenario{client: reuseClient, destination: M.ParseSocksaddrHostPort("127.0.0.1", 443)}.RoundTrip(t, "v5-tls-reuse-2")
	require.Equal(t, int32(1), proxy.count.Load())

	serverConn, err := net.Dial("tcp", serviceAddress)
	require.NoError(t, err)
	defer serverConn.Close()
	packetConn, err := client.DialPacketConn(serverConn)
	require.NoError(t, err)
	roundTripPacket(t, packetConn, serverConn, "v5-tls-udp-v4-wire")
}

func TestV5ServerAcceptsV4ClientLoopback(t *testing.T) {
	service, err := snellv5.NewService(snellv5.ServiceOptions{
		PSK:     []byte(testPSK),
		Handler: localEchoHandler{},
	})
	require.NoError(t, err)
	serviceAddress := startLocalSnellService(t, service)
	client, err := snellv4.NewClient(snellv4.ClientOptions{PSK: []byte(testPSK)})
	require.NoError(t, err)
	normalScenario{address: serviceAddress, client: client}.HalfCloseEcho(t, "v5-service-v4-client-tcp")

	var proxy countingTCPProxy
	proxy.Start(t, serviceAddress)
	reuseClient, err := snellv4.NewClient(snellv4.ClientOptions{
		PSK:    []byte(testPSK),
		Reuse:  true,
		Dialer: N.SystemDialer,
		Server: M.ParseSocksaddr(proxy.address),
	})
	require.NoError(t, err)
	defer reuseClient.Close()
	reuseScenario{client: reuseClient, destination: M.ParseSocksaddrHostPort("127.0.0.1", 443)}.RoundTrip(t, "v5-service-v4-client-reuse-1")
	reuseScenario{client: reuseClient, destination: M.ParseSocksaddrHostPort("127.0.0.1", 443)}.RoundTrip(t, "v5-service-v4-client-reuse-2")
	require.Equal(t, int32(1), proxy.count.Load())

	serverConn, err := net.Dial("tcp", serviceAddress)
	require.NoError(t, err)
	defer serverConn.Close()
	packetConn, err := client.DialPacketConn(serverConn)
	require.NoError(t, err)
	roundTripPacket(t, packetConn, serverConn, "v5-service-v4-client-udp")
}

func startLocalSnellService(t *testing.T, service snell.Service) string {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				serveErr := service.NewConnection(context.Background(), conn, M.SocksaddrFromNet(conn.RemoteAddr()), nil)
				if serveErr != nil {
					conn.Close()
				}
			}()
		}
	}()
	return listener.Addr().String()
}

func roundTripPacket(t *testing.T, packetConn N.NetPacketConn, serverConn net.Conn, label string) {
	message := make([]byte, 1200)
	_, err := rand.Read(message)
	require.NoError(t, err)
	target := M.ParseSocksaddrHostPort("203.0.113.1", 443).UDPAddr()
	_, err = packetConn.WriteTo(message, target)
	require.NoError(t, err)
	received := make([]byte, 2048)
	require.NoError(t, serverConn.SetReadDeadline(time.Now().Add(10*time.Second)))
	n, addr, err := packetConn.ReadFrom(received)
	require.NoError(t, err)
	require.Equal(t, target.String(), addr.String(), label)
	require.True(t, bytes.Equal(message, received[:n]), label)
}

func TestV5ServerReuseHandlerCloseBeforeClientEOF(t *testing.T) {
	service, err := snellv5.NewService(snellv5.ServiceOptions{
		PSK:     []byte(testPSK),
		Handler: partialEchoHandler{echoLen: 4 * 1024},
	})
	require.NoError(t, err)
	serviceAddress := startLocalSnellService(t, service)
	var proxy countingTCPProxy
	proxy.Start(t, serviceAddress)
	reuseClient, err := snellv4.NewClient(snellv4.ClientOptions{
		PSK:    []byte(testPSK),
		Reuse:  true,
		Dialer: N.SystemDialer,
		Server: M.ParseSocksaddr(proxy.address),
	})
	require.NoError(t, err)
	defer reuseClient.Close()
	scenario := reuseScenario{client: reuseClient, destination: M.ParseSocksaddrHostPort("127.0.0.1", 443)}
	scenario.PartialEchoRoundTrip(t, "v5-server-drain-1", 8*1024, 4*1024)
	scenario.PartialEchoRoundTrip(t, "v5-server-drain-2", 8*1024, 4*1024)
	require.Equal(t, int32(1), proxy.count.Load())
}

type localEchoHandler struct{}

func (localEchoHandler) NewConnectionEx(ctx context.Context, conn net.Conn, source M.Socksaddr, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	go func() {
		var closeErr error
		defer func() {
			writeCloseErr := N.CloseWrite(conn)
			if closeErr == nil {
				closeErr = writeCloseErr
			}
			connCloseErr := conn.Close()
			if closeErr == nil {
				closeErr = connCloseErr
			}
			if onClose != nil {
				onClose(closeErr)
			}
		}()
		payload, err := io.ReadAll(conn)
		if err != nil {
			closeErr = err
			return
		}
		_, err = io.Copy(conn, bytes.NewReader(payload))
		if err != nil {
			closeErr = err
		}
	}()
}

type partialEchoHandler struct {
	localEchoHandler
	echoLen int
}

func (h partialEchoHandler) NewConnectionEx(ctx context.Context, conn net.Conn, source M.Socksaddr, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	go func() {
		payload := make([]byte, h.echoLen)
		_, err := io.ReadFull(conn, payload)
		if err == nil {
			_, err = conn.Write(payload)
		}
		closeErr := conn.Close()
		if err == nil {
			err = closeErr
		}
		if onClose != nil {
			onClose(err)
		}
	}()
}

func (localEchoHandler) NewPacketConnectionEx(ctx context.Context, conn N.PacketConn, source M.Socksaddr, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	go func() {
		var closeErr error
		defer func() {
			connCloseErr := conn.Close()
			if closeErr == nil {
				closeErr = connCloseErr
			}
			if onClose != nil {
				onClose(closeErr)
			}
		}()
		for {
			select {
			case <-ctx.Done():
				closeErr = ctx.Err()
				return
			default:
			}
			buffer := buf.NewSize(64 * 1024)
			buffer.Resize(512, 0)
			packetDestination, err := conn.ReadPacket(buffer)
			if err != nil {
				buffer.Release()
				closeErr = err
				return
			}
			err = conn.WritePacket(buffer, packetDestination)
			if err != nil {
				closeErr = err
				return
			}
		}
	}()
}

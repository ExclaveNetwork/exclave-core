package test

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	snell "github.com/sagernet/sing-snell"
	"github.com/sagernet/sing-snell/snellv6"
	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/bufio"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	"github.com/stretchr/testify/require"
)

func v6Config(psk string, port uint16, mode string) string {
	return fmt.Sprintf("[snell-server]\nlisten = 0.0.0.0:%d\npsk = %s\nmode = %s\n", port, psk, mode)
}

var v6Modes = map[string]snellv6.Mode{
	"unshaped":   snellv6.ModeUnshaped,
	"unsafe-raw": snellv6.ModeUnsafeRaw,
	"default":    snellv6.ModeDefault,
}

func TestV6TCP(t *testing.T) {
	for name, mode := range v6Modes {
		t.Run(name, func(t *testing.T) {
			port := freePort(t)
			startSnellServer(t, "v6", v6Config(testPSK, port, name), port)
			echoPort := startTCPEcho(t)

			client, err := snellv6.NewClient(snellv6.ClientOptions{
				PSK:  []byte(testPSK),
				Mode: mode,
			})
			require.NoError(t, err)
			serverConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			require.NoError(t, err)
			defer serverConn.Close()

			proxyConn, err := client.DialConn(serverConn, M.ParseSocksaddrHostPort("127.0.0.1", echoPort))
			require.NoError(t, err)

			payload := make([]byte, 512*1024)
			rand.Read(payload)
			go func() {
				proxyConn.Write(payload)
			}()
			received := make([]byte, len(payload))
			proxyConn.SetReadDeadline(time.Now().Add(20 * time.Second))
			_, err = io.ReadFull(proxyConn, received)
			require.NoError(t, err)
			require.True(t, bytes.Equal(payload, received), "payload mismatch")
			normalScenario{address: fmt.Sprintf("127.0.0.1:%d", port), client: client}.HalfCloseEcho(t, "v6-official-"+name)
		})
	}
}

func TestV6TCPVectorisedWriter(t *testing.T) {
	for name, mode := range v6Modes {
		t.Run(name, func(t *testing.T) {
			port := freePort(t)
			startSnellServer(t, "v6", v6Config(testPSK, port, name), port)
			echoPort := startTCPEcho(t)

			client, err := snellv6.NewClient(snellv6.ClientOptions{
				PSK:  []byte(testPSK),
				Mode: mode,
			})
			require.NoError(t, err)
			serverConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			require.NoError(t, err)
			defer serverConn.Close()

			proxyConn := client.DialEarlyConn(serverConn, M.ParseSocksaddrHostPort("127.0.0.1", echoPort))
			vectorisedWriter, created := bufio.CreateVectorisedWriter(proxyConn)
			require.True(t, created)

			partA := make([]byte, 700)
			partB := make([]byte, 900)
			_, err = rand.Read(partA)
			require.NoError(t, err)
			_, err = rand.Read(partB)
			require.NoError(t, err)
			expected := append(append([]byte(nil), partA...), partB...)

			frontHeadroom := N.CalculateFrontHeadroom(proxyConn)
			rearHeadroom := N.CalculateRearHeadroom(proxyConn)
			buffers := make([]*buf.Buffer, 2)
			for index, payload := range [][]byte{partA, partB} {
				buffer := buf.NewSize(frontHeadroom + len(payload) + rearHeadroom)
				buffer.Resize(frontHeadroom, 0)
				_, err = buffer.Write(payload)
				require.NoError(t, err)
				buffers[index] = buffer
			}
			err = vectorisedWriter.WriteVectorised(buffers)
			require.NoError(t, err)

			received := make([]byte, len(expected))
			require.NoError(t, proxyConn.SetReadDeadline(time.Now().Add(10*time.Second)))
			_, err = io.ReadFull(proxyConn, received)
			require.NoError(t, err)
			require.Equal(t, expected, received)
		})
	}
}

func TestV6TCPUnshapedLargeWriteBufferFirstRecord(t *testing.T) {
	port := freePort(t)
	startSnellServer(t, "v6", v6Config(testPSK, port, "unshaped"), port)
	echoPort := startTCPEcho(t)

	client, err := snellv6.NewClient(snellv6.ClientOptions{
		PSK:  []byte(testPSK),
		Mode: snellv6.ModeUnshaped,
	})
	require.NoError(t, err)
	serverConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	require.NoError(t, err)
	defer serverConn.Close()

	destination := M.ParseSocksaddrHostPort("127.0.0.1", echoPort)
	proxyConn := client.DialEarlyConn(serverConn, destination)
	defer proxyConn.Close()

	payload := make([]byte, 80*1024)
	_, err = rand.Read(payload)
	require.NoError(t, err)
	request := snell.Request{Command: snell.CommandConnect, Destination: destination}
	buffer := buf.NewSize(request.Len() + len(payload))
	buffer.Resize(request.Len(), 0)
	n, err := buffer.Write(payload)
	require.NoError(t, err)
	require.Equal(t, len(payload), n)

	extendedWriter, ok := proxyConn.(N.ExtendedWriter)
	require.True(t, ok)
	err = extendedWriter.WriteBuffer(buffer)
	require.NoError(t, err)

	received := make([]byte, len(payload))
	require.NoError(t, proxyConn.SetReadDeadline(time.Now().Add(20*time.Second)))
	_, err = io.ReadFull(proxyConn, received)
	require.NoError(t, err)
	require.True(t, bytes.Equal(payload, received), "payload mismatch")
}

func TestV6TCPReuse(t *testing.T) {
	for name, mode := range v6Modes {
		t.Run(name, func(t *testing.T) {
			port := freePort(t)
			startSnellServer(t, "v6", v6Config(testPSK, port, name), port)
			var proxy countingTCPProxy
			proxy.Start(t, fmt.Sprintf("127.0.0.1:%d", port))
			echoPort := startTCPHalfCloseEcho(t)
			delayedEchoPort := startTCPHalfCloseEcho(t, 750*time.Millisecond)
			tailEchoPort := startTCPHalfCloseTailEcho(t, 32*1024, 250*time.Millisecond)
			oversizedTailEchoPort := startTCPHalfCloseTailEcho(t, 0x80001, 250*time.Millisecond)

			client, err := snellv6.NewClient(snellv6.ClientOptions{
				PSK:    []byte(testPSK),
				Mode:   mode,
				Reuse:  true,
				Dialer: N.SystemDialer,
				Server: M.ParseSocksaddr(proxy.address),
			})
			require.NoError(t, err)
			defer client.Close()

			scenario := reuseScenario{client: client, destination: M.ParseSocksaddrHostPort("127.0.0.1", echoPort)}
			scenario.RoundTrip(t, "v6-reuse-1-"+name)
			scenario.RoundTrip(t, "v6-reuse-2-"+name)
			delayedScenario := reuseScenario{client: client, destination: M.ParseSocksaddrHostPort("127.0.0.1", delayedEchoPort)}
			delayedScenario.CloseBeforeServerEOF(t, "v6-reuse-drain-"+name, time.Second)
			scenario.RoundTrip(t, "v6-reuse-after-drain-"+name)
			tailScenario := reuseScenario{client: client, destination: M.ParseSocksaddrHostPort("127.0.0.1", tailEchoPort)}
			tailScenario.CloseBeforeServerEOF(t, "v6-reuse-tail-"+name, 500*time.Millisecond)
			scenario.RoundTrip(t, "v6-reuse-after-tail-"+name)
			require.Equal(t, int32(1), proxy.count.Load())
			oversizedTailScenario := reuseScenario{client: client, destination: M.ParseSocksaddrHostPort("127.0.0.1", oversizedTailEchoPort)}
			oversizedTailScenario.CloseBeforeServerEOF(t, "v6-reuse-oversized-tail-"+name, 500*time.Millisecond)
			scenario.RoundTrip(t, "v6-reuse-after-oversized-tail-"+name)
			require.Equal(t, int32(2), proxy.count.Load())
		})
	}
}

func TestV6TCPReuseClientCloseCancelsDrain(t *testing.T) {
	for name, mode := range v6Modes {
		t.Run(name, func(t *testing.T) {
			port := freePort(t)
			startSnellServer(t, "v6", v6Config(testPSK, port, name), port)
			var proxy countingTCPProxy
			proxy.Start(t, fmt.Sprintf("127.0.0.1:%d", port))
			delayedEchoPort := startTCPHalfCloseEcho(t, 750*time.Millisecond)

			client, err := snellv6.NewClient(snellv6.ClientOptions{
				PSK:    []byte(testPSK),
				Mode:   mode,
				Reuse:  true,
				Dialer: N.SystemDialer,
				Server: M.ParseSocksaddr(proxy.address),
			})
			require.NoError(t, err)

			scenario := reuseScenario{client: client, destination: M.ParseSocksaddrHostPort("127.0.0.1", delayedEchoPort)}
			scenario.CloseBeforeServerEOF(t, "v6-reuse-close-cancel-"+name, 0)
			closeStart := time.Now()
			require.NoError(t, client.Close())
			if time.Since(closeStart) > 250*time.Millisecond {
				t.Fatal("client close waited for draining EOF instead of canceling the session")
			}
		})
	}
}

func TestV6TCPReuseClientCloseLetsActiveReadFinish(t *testing.T) {
	for name, mode := range v6Modes {
		t.Run(name, func(t *testing.T) {
			port := freePort(t)
			startSnellServer(t, "v6", v6Config(testPSK, port, name), port)
			var proxy countingTCPProxy
			proxy.Start(t, fmt.Sprintf("127.0.0.1:%d", port))
			delayedEchoPort := startTCPHalfCloseEcho(t, 750*time.Millisecond)
			echoPort := startTCPHalfCloseEcho(t)

			client, err := snellv6.NewClient(snellv6.ClientOptions{
				PSK:    []byte(testPSK),
				Mode:   mode,
				Reuse:  true,
				Dialer: N.SystemDialer,
				Server: M.ParseSocksaddr(proxy.address),
			})
			require.NoError(t, err)
			defer client.Close()

			reuseScenario{client: client, destination: M.ParseSocksaddrHostPort("127.0.0.1", delayedEchoPort)}.CloseWhileReadBlocked(t, "v6-reuse-active-read-close-"+name)
			reuseScenario{client: client, destination: M.ParseSocksaddrHostPort("127.0.0.1", echoPort)}.RoundTrip(t, "v6-reuse-after-active-read-close-"+name)
			require.Equal(t, int32(1), proxy.count.Load())
		})
	}
}

func TestV6ServerAcceptsV6ClientLoopback(t *testing.T) {
	for name, mode := range v6Modes {
		t.Run(name, func(t *testing.T) {
			service, err := snellv6.NewService(snellv6.ServerOptions{
				PSK:     []byte(testPSK),
				Mode:    mode,
				Handler: localEchoHandler{},
			})
			require.NoError(t, err)
			serviceAddress := startLocalSnellService(t, service)
			var proxy countingTCPProxy
			proxy.Start(t, serviceAddress)

			client, err := snellv6.NewClient(snellv6.ClientOptions{
				PSK:    []byte(testPSK),
				Mode:   mode,
				Reuse:  true,
				Dialer: N.SystemDialer,
				Server: M.ParseSocksaddr(proxy.address),
			})
			require.NoError(t, err)
			defer client.Close()

			scenario := reuseScenario{client: client, destination: M.ParseSocksaddrHostPort("127.0.0.1", 443)}
			scenario.RoundTrip(t, "v6-service-v6-client-reuse-1-"+name)
			scenario.RoundTrip(t, "v6-service-v6-client-reuse-2-"+name)
			require.Equal(t, int32(1), proxy.count.Load())
		})
	}
}

func TestV6ServerReuseHandlerCloseBeforeClientEOF(t *testing.T) {
	for name, mode := range v6Modes {
		t.Run(name, func(t *testing.T) {
			service, err := snellv6.NewService(snellv6.ServerOptions{
				PSK:     []byte(testPSK),
				Mode:    mode,
				Handler: partialEchoHandler{echoLen: 4 * 1024},
			})
			require.NoError(t, err)
			serviceAddress := startLocalSnellService(t, service)
			var proxy countingTCPProxy
			proxy.Start(t, serviceAddress)

			client, err := snellv6.NewClient(snellv6.ClientOptions{
				PSK:    []byte(testPSK),
				Mode:   mode,
				Reuse:  true,
				Dialer: N.SystemDialer,
				Server: M.ParseSocksaddr(proxy.address),
			})
			require.NoError(t, err)
			defer client.Close()

			scenario := reuseScenario{client: client, destination: M.ParseSocksaddrHostPort("127.0.0.1", 443)}
			scenario.PartialEchoRoundTrip(t, "v6-server-drain-1-"+name, 8*1024, 4*1024)
			scenario.PartialEchoRoundTrip(t, "v6-server-drain-2-"+name, 8*1024, 4*1024)
			require.Equal(t, int32(1), proxy.count.Load())
		})
	}
}

func TestV6UDP(t *testing.T) {
	for name, mode := range v6Modes {
		t.Run(name, func(t *testing.T) {
			port := freePort(t)
			startSnellServer(t, "v6", v6Config(testPSK, port, name), port)
			echoPort := startUDPEcho(t)

			client, err := snellv6.NewClient(snellv6.ClientOptions{
				PSK:  []byte(testPSK),
				Mode: mode,
			})
			require.NoError(t, err)
			serverConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			require.NoError(t, err)
			defer serverConn.Close()

			packetConn, err := client.DialPacketConn(serverConn)
			require.NoError(t, err)
			target := M.ParseSocksaddrHostPort("127.0.0.1", echoPort).UDPAddr()

			for round := range 10 {
				messageLen := 1200
				if mode == snellv6.ModeDefault {
					messageLen = 32
				}
				message := make([]byte, messageLen)
				rand.Read(message)
				_, err = packetConn.WriteTo(message, target)
				require.NoError(t, err)

				received := make([]byte, 2048)
				serverConn.SetReadDeadline(time.Now().Add(10 * time.Second))
				n, _, readErr := packetConn.ReadFrom(received)
				require.NoError(t, readErr)
				require.True(t, bytes.Equal(message, received[:n]), "round %d", round)
			}
		})
	}
}

func TestV6UDPPacketBatchWriter(t *testing.T) {
	for name, mode := range v6Modes {
		t.Run(name, func(t *testing.T) {
			port := freePort(t)
			startSnellServer(t, "v6", v6Config(testPSK, port, name), port)
			echoPort := startUDPEcho(t)

			client, err := snellv6.NewClient(snellv6.ClientOptions{
				PSK:  []byte(testPSK),
				Mode: mode,
			})
			require.NoError(t, err)
			serverConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			require.NoError(t, err)
			defer serverConn.Close()

			packetConn, err := client.DialPacketConn(serverConn)
			require.NoError(t, err)
			batchWriter, created := bufio.CreatePacketBatchWriter(packetConn)
			require.True(t, created)

			target := M.ParseSocksaddrHostPort("127.0.0.1", echoPort)
			frontHeadroom := N.CalculateFrontHeadroom(packetConn)
			rearHeadroom := N.CalculateRearHeadroom(packetConn)
			payloadSize := 1200
			if mode == snellv6.ModeDefault {
				payloadSize = 32
			}
			payloads := make([][]byte, 3)
			buffers := make([]*buf.Buffer, len(payloads))
			destinations := make([]M.Socksaddr, len(payloads))
			for index := range payloads {
				payload := make([]byte, payloadSize)
				_, err = rand.Read(payload)
				require.NoError(t, err)
				payload[0] = byte(index)
				payloads[index] = payload
				buffer := buf.NewSize(frontHeadroom + len(payload) + rearHeadroom)
				buffer.Resize(frontHeadroom, 0)
				_, err = buffer.Write(payload)
				require.NoError(t, err)
				buffers[index] = buffer
				destinations[index] = target
			}
			err = batchWriter.WritePacketBatch(buffers, destinations)
			require.NoError(t, err)

			received := make(map[string]int, len(payloads))
			for range len(payloads) {
				reply := make([]byte, 2048)
				err = packetConn.SetReadDeadline(time.Now().Add(10 * time.Second))
				require.NoError(t, err)
				n, _, readErr := packetConn.ReadFrom(reply)
				require.NoError(t, readErr)
				received[string(reply[:n])]++
			}
			for _, payload := range payloads {
				require.Equal(t, 1, received[string(payload)])
			}
		})
	}
}

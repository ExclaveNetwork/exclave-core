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
	"github.com/sagernet/sing-snell/snellv4"
	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/bufio"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	"github.com/stretchr/testify/require"
)

func TestV4TCP(t *testing.T) {
	port := freePort(t)
	config := fmt.Sprintf("[snell-server]\nlisten = 0.0.0.0:%d\npsk = %s\nipv6 = false\n", port, testPSK)
	startSnellServer(t, "v4", config, port)
	echoPort := startTCPEcho(t)

	client, err := snellv4.NewClient(snellv4.ClientOptions{PSK: []byte(testPSK)})
	require.NoError(t, err)
	serverConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	require.NoError(t, err)
	defer serverConn.Close()

	proxyConn, err := client.DialConn(serverConn, M.ParseSocksaddrHostPort("127.0.0.1", echoPort))
	require.NoError(t, err)

	payload := make([]byte, 1024*1024)
	rand.Read(payload)
	go func() {
		proxyConn.Write(payload)
	}()
	received := make([]byte, len(payload))
	proxyConn.SetReadDeadline(time.Now().Add(20 * time.Second))
	_, err = io.ReadFull(proxyConn, received)
	require.NoError(t, err)
	require.True(t, bytes.Equal(payload, received), "payload mismatch")
	normalScenario{address: fmt.Sprintf("127.0.0.1:%d", port), client: client}.HalfCloseEcho(t, "v4-official")
}

func TestV4TCPReuse(t *testing.T) {
	port := freePort(t)
	config := fmt.Sprintf("[snell-server]\nlisten = 0.0.0.0:%d\npsk = %s\nipv6 = false\n", port, testPSK)
	startSnellServer(t, "v4", config, port)
	var proxy countingTCPProxy
	proxy.Start(t, fmt.Sprintf("127.0.0.1:%d", port))
	echoPort := startTCPHalfCloseEcho(t)
	delayedEchoPort := startTCPHalfCloseEcho(t, 750*time.Millisecond)
	tailEchoPort := startTCPHalfCloseTailEcho(t, 32*1024, 250*time.Millisecond)
	oversizedTailEchoPort := startTCPHalfCloseTailEcho(t, 0x80001, 250*time.Millisecond)

	client, err := snellv4.NewClient(snellv4.ClientOptions{
		PSK:    []byte(testPSK),
		Reuse:  true,
		Dialer: N.SystemDialer,
		Server: M.ParseSocksaddr(proxy.address),
	})
	require.NoError(t, err)
	defer client.Close()

	scenario := reuseScenario{client: client, destination: M.ParseSocksaddrHostPort("127.0.0.1", echoPort)}
	scenario.RoundTrip(t, "v4-reuse-1")
	scenario.RoundTrip(t, "v4-reuse-2")
	delayedScenario := reuseScenario{client: client, destination: M.ParseSocksaddrHostPort("127.0.0.1", delayedEchoPort)}
	delayedScenario.CloseBeforeServerEOF(t, "v4-reuse-drain", time.Second)
	scenario.RoundTrip(t, "v4-reuse-after-drain")
	tailScenario := reuseScenario{client: client, destination: M.ParseSocksaddrHostPort("127.0.0.1", tailEchoPort)}
	tailScenario.CloseBeforeServerEOF(t, "v4-reuse-tail", 500*time.Millisecond)
	scenario.RoundTrip(t, "v4-reuse-after-tail")
	require.Equal(t, int32(1), proxy.count.Load())
	oversizedTailScenario := reuseScenario{client: client, destination: M.ParseSocksaddrHostPort("127.0.0.1", oversizedTailEchoPort)}
	oversizedTailScenario.CloseBeforeServerEOF(t, "v4-reuse-oversized-tail", 500*time.Millisecond)
	scenario.RoundTrip(t, "v4-reuse-after-oversized-tail")
	require.Equal(t, int32(2), proxy.count.Load())
}

func TestV4TCPReuseClientCloseCancelsDrain(t *testing.T) {
	port := freePort(t)
	config := fmt.Sprintf("[snell-server]\nlisten = 0.0.0.0:%d\npsk = %s\nipv6 = false\n", port, testPSK)
	startSnellServer(t, "v4", config, port)
	var proxy countingTCPProxy
	proxy.Start(t, fmt.Sprintf("127.0.0.1:%d", port))
	delayedEchoPort := startTCPHalfCloseEcho(t, 750*time.Millisecond)

	client, err := snellv4.NewClient(snellv4.ClientOptions{
		PSK:    []byte(testPSK),
		Reuse:  true,
		Dialer: N.SystemDialer,
		Server: M.ParseSocksaddr(proxy.address),
	})
	require.NoError(t, err)

	scenario := reuseScenario{client: client, destination: M.ParseSocksaddrHostPort("127.0.0.1", delayedEchoPort)}
	scenario.CloseBeforeServerEOF(t, "v4-reuse-close-cancel", 0)
	closeStart := time.Now()
	require.NoError(t, client.Close())
	if time.Since(closeStart) > 250*time.Millisecond {
		t.Fatal("client close waited for draining EOF instead of canceling the session")
	}
}

func TestV4TCPReuseClientCloseLetsActiveReadFinish(t *testing.T) {
	port := freePort(t)
	config := fmt.Sprintf("[snell-server]\nlisten = 0.0.0.0:%d\npsk = %s\nipv6 = false\n", port, testPSK)
	startSnellServer(t, "v4", config, port)
	var proxy countingTCPProxy
	proxy.Start(t, fmt.Sprintf("127.0.0.1:%d", port))
	delayedEchoPort := startTCPHalfCloseEcho(t, 750*time.Millisecond)
	echoPort := startTCPHalfCloseEcho(t)

	client, err := snellv4.NewClient(snellv4.ClientOptions{
		PSK:    []byte(testPSK),
		Reuse:  true,
		Dialer: N.SystemDialer,
		Server: M.ParseSocksaddr(proxy.address),
	})
	require.NoError(t, err)
	defer client.Close()

	reuseScenario{client: client, destination: M.ParseSocksaddrHostPort("127.0.0.1", delayedEchoPort)}.CloseWhileReadBlocked(t, "v4-reuse-active-read-close")
	reuseScenario{client: client, destination: M.ParseSocksaddrHostPort("127.0.0.1", echoPort)}.RoundTrip(t, "v4-reuse-after-active-read-close")
	require.Equal(t, int32(1), proxy.count.Load())
}

func TestV4UDP(t *testing.T) {
	port := freePort(t)
	config := fmt.Sprintf("[snell-server]\nlisten = 0.0.0.0:%d\npsk = %s\nipv6 = false\n", port, testPSK)
	startSnellServer(t, "v4", config, port)
	echoPort := startUDPEcho(t)

	client, err := snellv4.NewClient(snellv4.ClientOptions{PSK: []byte(testPSK)})
	require.NoError(t, err)
	serverConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	require.NoError(t, err)
	defer serverConn.Close()

	packetConn, err := client.DialPacketConn(serverConn)
	require.NoError(t, err)
	target := M.ParseSocksaddrHostPort("127.0.0.1", echoPort).UDPAddr()

	for round := range 10 {
		message := make([]byte, 1200)
		rand.Read(message)
		_, err = packetConn.WriteTo(message, target)
		require.NoError(t, err)

		received := make([]byte, 2048)
		serverConn.SetReadDeadline(time.Now().Add(10 * time.Second))
		n, _, readErr := packetConn.ReadFrom(received)
		require.NoError(t, readErr)
		require.True(t, bytes.Equal(message, received[:n]), "round %d", round)
	}
}

func TestV4TCPVectorisedWriter(t *testing.T) {
	port := freePort(t)
	config := fmt.Sprintf("[snell-server]\nlisten = 0.0.0.0:%d\npsk = %s\nipv6 = false\n", port, testPSK)
	startSnellServer(t, "v4", config, port)
	echoPort := startTCPEcho(t)

	client, err := snellv4.NewClient(snellv4.ClientOptions{PSK: []byte(testPSK)})
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
}

func TestV4UDPPacketBatchWriter(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		version string
	}{
		{name: "official-v4", version: "v4"},
		{name: "official-v5", version: "v5"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			port := freePort(t)
			switch testCase.version {
			case "v4":
				config := fmt.Sprintf("[snell-server]\nlisten = 0.0.0.0:%d\npsk = %s\nipv6 = false\n", port, testPSK)
				startSnellServer(t, "v4", config, port)
			case "v5":
				startSnellServer(t, "v5", v5Config(testPSK, port), port)
			default:
				t.Fatal("unknown server version")
			}
			echoPort := startUDPEcho(t)

			client, err := snellv4.NewClient(snellv4.ClientOptions{PSK: []byte(testPSK)})
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
			payloads := make([][]byte, 3)
			buffers := make([]*buf.Buffer, len(payloads))
			destinations := make([]M.Socksaddr, len(payloads))
			for index := range payloads {
				payload := make([]byte, 1200)
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

func TestV4ObfsHTTP(t *testing.T) {
	t.Run("tcp", func(t *testing.T) {
		port := freePort(t)
		config := fmt.Sprintf("[snell-server]\nlisten = 0.0.0.0:%d\npsk = %s\nobfs = http\nipv6 = false\n", port, testPSK)
		startSnellServer(t, "v4", config, port)
		echoPort := startTCPEcho(t)

		client, err := snellv4.NewClient(snellv4.ClientOptions{
			PSK:      []byte(testPSK),
			ObfsMode: snell.ObfsModeHTTP,
			ObfsHost: "obfs.example",
		})
		require.NoError(t, err)
		serverConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		require.NoError(t, err)
		defer serverConn.Close()

		proxyConn, err := client.DialConn(serverConn, M.ParseSocksaddrHostPort("127.0.0.1", echoPort))
		require.NoError(t, err)

		payload := make([]byte, 256*1024)
		rand.Read(payload)
		go func() {
			proxyConn.Write(payload)
		}()
		received := make([]byte, len(payload))
		proxyConn.SetReadDeadline(time.Now().Add(20 * time.Second))
		_, err = io.ReadFull(proxyConn, received)
		require.NoError(t, err)
		require.True(t, bytes.Equal(payload, received), "payload mismatch")
		normalScenario{address: fmt.Sprintf("127.0.0.1:%d", port), client: client}.HalfCloseEcho(t, "v4-official-obfs")
	})

	t.Run("reuse", func(t *testing.T) {
		port := freePort(t)
		config := fmt.Sprintf("[snell-server]\nlisten = 0.0.0.0:%d\npsk = %s\nobfs = http\nipv6 = false\n", port, testPSK)
		startSnellServer(t, "v4", config, port)
		var proxy countingTCPProxy
		proxy.Start(t, fmt.Sprintf("127.0.0.1:%d", port))
		echoPort := startTCPHalfCloseEcho(t)

		client, err := snellv4.NewClient(snellv4.ClientOptions{
			PSK:      []byte(testPSK),
			Reuse:    true,
			ObfsMode: snell.ObfsModeHTTP,
			ObfsHost: "obfs.example",
			Dialer:   N.SystemDialer,
			Server:   M.ParseSocksaddr(proxy.address),
		})
		require.NoError(t, err)
		defer client.Close()

		scenario := reuseScenario{client: client, destination: M.ParseSocksaddrHostPort("127.0.0.1", echoPort)}
		scenario.RoundTrip(t, "v4-obfs-reuse-1")
		scenario.RoundTrip(t, "v4-obfs-reuse-2")
		require.Equal(t, int32(1), proxy.count.Load())
	})

	t.Run("udp", func(t *testing.T) {
		port := freePort(t)
		config := fmt.Sprintf("[snell-server]\nlisten = 0.0.0.0:%d\npsk = %s\nobfs = http\nipv6 = false\n", port, testPSK)
		startSnellServer(t, "v4", config, port)
		echoPort := startUDPEcho(t)

		client, err := snellv4.NewClient(snellv4.ClientOptions{
			PSK:      []byte(testPSK),
			ObfsMode: snell.ObfsModeHTTP,
			ObfsHost: "obfs.example",
		})
		require.NoError(t, err)
		serverConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		require.NoError(t, err)
		defer serverConn.Close()

		packetConn, err := client.DialPacketConn(serverConn)
		require.NoError(t, err)
		target := M.ParseSocksaddrHostPort("127.0.0.1", echoPort).UDPAddr()

		for round := range 5 {
			message := make([]byte, 1200)
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

package test

import (
	"bytes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	snell "github.com/sagernet/sing-snell"
	"github.com/sagernet/sing-snell/snellv4"
	"github.com/sagernet/sing-snell/snellv5"
	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/bufio"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	"github.com/stretchr/testify/require"
)

const testPSK = "snell-interop-test-psk-0123456789"

var errV5RawZeroChunk = errors.New("snell v5 raw zero chunk")

func v5Config(psk string, port uint16) string {
	return fmt.Sprintf("[snell-server]\nlisten = 0.0.0.0:%d\npsk = %s\nipv6 = false\n", port, psk)
}

func TestV5FirstResponseRecordShape(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		official bool
	}{
		{name: "official-v5.0.1", official: true},
		{name: "local-v5"},
	} {
		t.Run(testCase.name, func(subTest *testing.T) {
			var serverAddress string
			var targetPort uint16
			if testCase.official {
				port := freePort(subTest)
				startSnellServer(subTest, "v5", v5Config(testPSK, port), port)
				serverAddress = fmt.Sprintf("127.0.0.1:%d", port)
				targetPort = startTCPEcho(subTest)
			} else {
				service, err := snellv5.NewService(snellv5.ServiceOptions{
					PSK:     []byte(testPSK),
					Handler: localEchoHandler{},
				})
				require.NoError(subTest, err)
				serverAddress = startLocalSnellService(subTest, service)
				targetPort = 443
			}

			serverConn, err := net.Dial("tcp", serverAddress)
			require.NoError(subTest, err)
			defer serverConn.Close()

			earlyPayload := []byte("v5 first response record shape")
			host := "127.0.0.1"
			requestPlain := []byte{snell.RequestVersion, snell.CommandConnectV2, 0, byte(len(host))}
			requestPlain = append(requestPlain, host...)
			requestPlain = binary.BigEndian.AppendUint16(requestPlain, targetPort)
			requestPlain = append(requestPlain, earlyPayload...)

			clientSalt := make([]byte, snell.SaltLen)
			_, err = rand.Read(clientSalt)
			require.NoError(subTest, err)
			clientAEAD, err := snell.NewAEAD(snell.DeriveKey([]byte(testPSK), clientSalt))
			require.NoError(subTest, err)
			clientNonce := make([]byte, snell.NonceLen)
			requestHeader := make([]byte, snell.HeaderPlainLen)
			requestHeader[0] = snell.HeaderVersion
			binary.BigEndian.PutUint16(requestHeader[5:7], uint16(len(requestPlain)))
			clientWire := make([]byte, 0, snell.SaltLen+snell.HeaderCipherLen+len(requestPlain)+snell.AEADTagLen+snell.HeaderCipherLen)
			clientWire = append(clientWire, clientSalt...)
			clientWire = append(clientWire, clientAEAD.Seal(nil, clientNonce, requestHeader, nil)...)
			snell.IncreaseNonce(clientNonce)
			clientWire = append(clientWire, clientAEAD.Seal(nil, clientNonce, requestPlain, nil)...)
			snell.IncreaseNonce(clientNonce)
			clear(requestHeader)
			requestHeader[0] = snell.HeaderVersion
			clientWire = append(clientWire, clientAEAD.Seal(nil, clientNonce, requestHeader, nil)...)

			_, err = serverConn.Write(clientWire)
			require.NoError(subTest, err)
			err = serverConn.SetReadDeadline(time.Now().Add(10 * time.Second))
			require.NoError(subTest, err)

			responseSalt := make([]byte, snell.SaltLen)
			_, err = io.ReadFull(serverConn, responseSalt)
			require.NoError(subTest, err)
			responseAEAD, err := snell.NewAEAD(snell.DeriveKey([]byte(testPSK), responseSalt))
			require.NoError(subTest, err)
			responseNonce := make([]byte, snell.NonceLen)
			responseHeaderCipher := make([]byte, snell.HeaderCipherLen)
			_, err = io.ReadFull(serverConn, responseHeaderCipher)
			require.NoError(subTest, err)
			responseHeader, err := responseAEAD.Open(nil, responseNonce, responseHeaderCipher, nil)
			require.NoError(subTest, err)
			snell.IncreaseNonce(responseNonce)
			require.Len(subTest, responseHeader, snell.HeaderPlainLen)
			require.Equal(subTest, byte(snell.HeaderVersion), responseHeader[0])
			require.Equal(subTest, byte(0), responseHeader[1])
			require.Equal(subTest, byte(0), responseHeader[2])
			// snell-server v5.0.1: FUN_00139670 subtracts first-record padding
			// from the first payload limit, and FUN_00138af0 serializes and mixes it.
			responsePaddingLen := int(binary.BigEndian.Uint16(responseHeader[3:5]))
			require.GreaterOrEqual(subTest, responsePaddingLen, 0x100)
			require.LessOrEqual(subTest, responsePaddingLen, 0x1ff)
			responsePayloadLen := int(binary.BigEndian.Uint16(responseHeader[5:7]))
			require.Equal(subTest, 1+len(earlyPayload), responsePayloadLen)
			responsePadding := make([]byte, responsePaddingLen)
			_, err = io.ReadFull(serverConn, responsePadding)
			require.NoError(subTest, err)
			responsePayloadCipher := make([]byte, responsePayloadLen+snell.AEADTagLen)
			_, err = io.ReadFull(serverConn, responsePayloadCipher)
			require.NoError(subTest, err)
			mixLimit := min(len(responsePadding), len(responsePayloadCipher))
			for index := 0; index < mixLimit; index += 2 {
				responsePadding[index], responsePayloadCipher[index] = responsePayloadCipher[index], responsePadding[index]
			}
			responsePayload, err := responseAEAD.Open(nil, responseNonce, responsePayloadCipher, nil)
			require.NoError(subTest, err)
			expectedPayload := append([]byte{snell.ReplyTunnel}, earlyPayload...)
			require.Equal(subTest, expectedPayload, responsePayload)
		})
	}
}

func TestV5TCP(t *testing.T) {
	port := freePort(t)
	startSnellServer(t, "v5", v5Config(testPSK, port), port)
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
	normalScenario{address: fmt.Sprintf("127.0.0.1:%d", port), client: client}.HalfCloseEcho(t, "v5-official")
}

func TestV5TCPVectorisedWriter(t *testing.T) {
	port := freePort(t)
	startSnellServer(t, "v5", v5Config(testPSK, port), port)
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

func TestV5TCPReuse(t *testing.T) {
	port := freePort(t)
	startSnellServer(t, "v5", v5Config(testPSK, port), port)
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
	scenario.RoundTrip(t, "v5-reuse-1")
	scenario.RoundTrip(t, "v5-reuse-2")
	delayedScenario := reuseScenario{client: client, destination: M.ParseSocksaddrHostPort("127.0.0.1", delayedEchoPort)}
	delayedScenario.CloseBeforeServerEOF(t, "v5-reuse-drain", time.Second)
	scenario.RoundTrip(t, "v5-reuse-after-drain")
	tailScenario := reuseScenario{client: client, destination: M.ParseSocksaddrHostPort("127.0.0.1", tailEchoPort)}
	tailScenario.CloseBeforeServerEOF(t, "v5-reuse-tail", 500*time.Millisecond)
	scenario.RoundTrip(t, "v5-reuse-after-tail")
	require.Equal(t, int32(1), proxy.count.Load())
	oversizedTailScenario := reuseScenario{client: client, destination: M.ParseSocksaddrHostPort("127.0.0.1", oversizedTailEchoPort)}
	oversizedTailScenario.CloseBeforeServerEOF(t, "v5-reuse-oversized-tail", 500*time.Millisecond)
	scenario.RoundTrip(t, "v5-reuse-after-oversized-tail")
	require.Equal(t, int32(2), proxy.count.Load())
}

func TestV5TCPReuseClientCloseLetsActiveReadFinish(t *testing.T) {
	port := freePort(t)
	startSnellServer(t, "v5", v5Config(testPSK, port), port)
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

	reuseScenario{client: client, destination: M.ParseSocksaddrHostPort("127.0.0.1", delayedEchoPort)}.CloseWhileReadBlocked(t, "v5-reuse-active-read-close")
	reuseScenario{client: client, destination: M.ParseSocksaddrHostPort("127.0.0.1", echoPort)}.RoundTrip(t, "v5-reuse-after-active-read-close")
	require.Equal(t, int32(1), proxy.count.Load())
}

func TestV5ObfsHTTP(t *testing.T) {
	t.Run("tcp", func(t *testing.T) {
		port := freePort(t)
		startSnellServer(t, "v5", v5Config(testPSK, port)+"obfs = http\n", port)
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
		normalScenario{address: fmt.Sprintf("127.0.0.1:%d", port), client: client}.HalfCloseEcho(t, "v5-official-obfs")
	})

	t.Run("reuse", func(t *testing.T) {
		port := freePort(t)
		startSnellServer(t, "v5", v5Config(testPSK, port)+"obfs = http\n", port)
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
		scenario.RoundTrip(t, "v5-obfs-reuse-1")
		scenario.RoundTrip(t, "v5-obfs-reuse-2")
		require.Equal(t, int32(1), proxy.count.Load())
	})
}

func TestV5UDP(t *testing.T) {
	port := freePort(t)
	startSnellServer(t, "v5", v5Config(testPSK, port), port)
	echoPort := startUDPEcho(t)

	client, err := snellv4.NewClient(snellv4.ClientOptions{PSK: []byte(testPSK)})
	require.NoError(t, err)
	target := M.ParseSocksaddrHostPort("127.0.0.1", echoPort).UDPAddr()

	message := make([]byte, 1200)
	rand.Read(message)
	serverConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	require.NoError(t, err)
	defer serverConn.Close()
	packetConn, err := client.DialPacketConn(serverConn)
	require.NoError(t, err)
	_, err = packetConn.WriteTo(message, target)
	require.NoError(t, err)

	received := make([]byte, 2048)
	serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := packetConn.ReadFrom(received)
	require.NoError(t, err)
	require.True(t, bytes.Equal(message, received[:n]))
}

func TestV5PingReplyClosesConnection(t *testing.T) {
	for _, testCase := range []struct {
		name string
		run  func(*testing.T) string
	}{
		{
			name: "official-v5.0.1",
			run: func(t *testing.T) string {
				port := freePort(t)
				startSnellServer(t, "v5", v5Config(testPSK, port), port)
				return fmt.Sprintf("127.0.0.1:%d", port)
			},
		},
		{
			name: "local-v5",
			run: func(t *testing.T) string {
				service, err := snellv5.NewService(snellv5.ServiceOptions{
					PSK:     []byte(testPSK),
					Handler: localEchoHandler{},
				})
				require.NoError(t, err)
				return startLocalSnellService(t, service)
			},
		},
		{
			name: "local-v5-multi-without-user",
			run: func(t *testing.T) string {
				service, err := snellv5.NewMultiService[int](snellv5.ServiceOptions{
					PSK:     []byte(testPSK),
					Handler: localEchoHandler{},
				})
				require.NoError(t, err)
				return startLocalSnellService(t, service)
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			serverConn, err := net.Dial("tcp", testCase.run(t))
			require.NoError(t, err)
			defer serverConn.Close()

			rawConn := &v5RawConn{Conn: serverConn, psk: []byte(testPSK)}
			salt := make([]byte, snell.SaltLen)
			_, err = rand.Read(salt)
			require.NoError(t, err)
			rawConn.WriteFirstRecord(t, salt, []byte{snell.RequestVersion, snell.CommandPing, 1, 'x'})
			require.NoError(t, serverConn.SetReadDeadline(time.Now().Add(10*time.Second)))
			reply, err := rawConn.ReadFirstRecord(t)
			require.NoError(t, err)
			require.Equal(t, []byte{snell.ReplyPong}, reply)

			_, err = rawConn.ReadRecord(t)
			require.Error(t, err)
			require.False(t, errors.Is(err, errV5RawZeroChunk), "ping closes the connection instead of writing a Snell zero chunk")
		})
	}
}

func TestV5LegacyCommandConnectClosesWithoutZeroChunkAfterReply(t *testing.T) {
	for _, testCase := range []struct {
		name string
		run  func(*testing.T) (string, uint16)
	}{
		{
			name: "official-v5.0.1",
			run: func(t *testing.T) (string, uint16) {
				port := freePort(t)
				startSnellServer(t, "v5", v5Config(testPSK, port), port)
				return fmt.Sprintf("127.0.0.1:%d", port), startTCPHalfCloseEcho(t)
			},
		},
		{
			name: "local-v5",
			run: func(t *testing.T) (string, uint16) {
				service, err := snellv5.NewService(snellv5.ServiceOptions{
					PSK:     []byte(testPSK),
					Handler: localEchoHandler{},
				})
				require.NoError(t, err)
				return startLocalSnellService(t, service), 443
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			serverAddress, targetPort := testCase.run(t)
			serverConn, err := net.Dial("tcp", serverAddress)
			require.NoError(t, err)
			defer serverConn.Close()

			payload := []byte("v5 legacy command connect")
			host := "127.0.0.1"
			request := []byte{snell.RequestVersion, snell.CommandConnect, 0, byte(len(host))}
			request = append(request, host...)
			request = binary.BigEndian.AppendUint16(request, targetPort)
			request = append(request, payload...)

			rawConn := &v5RawConn{Conn: serverConn, psk: []byte(testPSK)}
			salt := make([]byte, snell.SaltLen)
			_, err = rand.Read(salt)
			require.NoError(t, err)
			rawConn.WriteFirstRecord(t, salt, request)
			rawConn.WriteRecord(t, nil)
			require.NoError(t, serverConn.SetReadDeadline(time.Now().Add(10*time.Second)))
			reply, err := rawConn.ReadFirstRecord(t)
			require.NoError(t, err)
			require.Equal(t, append([]byte{snell.ReplyTunnel}, payload...), reply)

			_, err = rawConn.ReadRecord(t)
			require.Error(t, err)
			require.False(t, errors.Is(err, errV5RawZeroChunk), "legacy command-1 closes the TCP connection instead of writing a Snell zero chunk")
		})
	}
}

func TestV5SaltReplayRejectsDuplicate(t *testing.T) {
	for _, testCase := range []struct {
		name string
		run  func(*testing.T) (string, uint16)
	}{
		{
			name: "official-v5.0.1",
			run: func(t *testing.T) (string, uint16) {
				port := freePort(t)
				startSnellServer(t, "v5", v5Config(testPSK, port), port)
				return fmt.Sprintf("127.0.0.1:%d", port), startTCPHalfCloseEcho(t)
			},
		},
		{
			name: "local-v5",
			run: func(t *testing.T) (string, uint16) {
				service, err := snellv5.NewService(snellv5.ServiceOptions{
					PSK:     []byte(testPSK),
					Handler: localEchoHandler{},
				})
				require.NoError(t, err)
				return startLocalSnellService(t, service), 443
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			serverAddress, targetPort := testCase.run(t)
			payload := []byte("v5 duplicated salt")
			host := "127.0.0.1"
			request := []byte{snell.RequestVersion, snell.CommandConnectV2, 0, byte(len(host))}
			request = append(request, host...)
			request = binary.BigEndian.AppendUint16(request, targetPort)
			request = append(request, payload...)
			salt := make([]byte, snell.SaltLen)
			_, err := rand.Read(salt)
			require.NoError(t, err)

			firstServerConn, err := net.Dial("tcp", serverAddress)
			require.NoError(t, err)
			firstRawConn := &v5RawConn{Conn: firstServerConn, psk: []byte(testPSK)}
			firstRawConn.WriteFirstRecord(t, salt, request)
			firstRawConn.WriteRecord(t, nil)
			require.NoError(t, firstServerConn.SetReadDeadline(time.Now().Add(10*time.Second)))
			reply, err := firstRawConn.ReadFirstRecord(t)
			require.NoError(t, err)
			require.Equal(t, append([]byte{snell.ReplyTunnel}, payload...), reply)
			firstServerConn.Close()

			secondServerConn, err := net.Dial("tcp", serverAddress)
			require.NoError(t, err)
			defer secondServerConn.Close()
			secondRawConn := &v5RawConn{Conn: secondServerConn, psk: []byte(testPSK)}
			secondRawConn.WriteFirstRecord(t, salt, request)
			secondRawConn.WriteRecord(t, nil)
			require.NoError(t, secondServerConn.SetReadDeadline(time.Now().Add(10*time.Second)))
			_, err = secondRawConn.ReadFirstRecord(t)
			require.Error(t, err)
		})
	}
}

type v5RawConn struct {
	net.Conn
	psk           []byte
	requestAEAD   cipher.AEAD
	requestNonce  []byte
	responseAEAD  cipher.AEAD
	responseNonce []byte
}

func (c *v5RawConn) WriteFirstRecord(t *testing.T, salt []byte, payload []byte) {
	requestAEAD, err := snell.NewAEAD(snell.DeriveKey(c.psk, salt))
	require.NoError(t, err)
	c.requestAEAD = requestAEAD
	c.requestNonce = make([]byte, snell.NonceLen)
	record := c.SealRecord(t, payload)
	wire := make([]byte, 0, len(salt)+len(record))
	wire = append(wire, salt...)
	wire = append(wire, record...)
	_, err = c.Conn.Write(wire)
	require.NoError(t, err)
}

func (c *v5RawConn) WriteRecord(t *testing.T, payload []byte) {
	_, err := c.Conn.Write(c.SealRecord(t, payload))
	require.NoError(t, err)
}

func (c *v5RawConn) SealRecord(t *testing.T, payload []byte) []byte {
	header := make([]byte, snell.HeaderPlainLen)
	header[0] = snell.HeaderVersion
	binary.BigEndian.PutUint16(header[5:7], uint16(len(payload)))
	record := c.requestAEAD.Seal(nil, c.requestNonce, header, nil)
	snell.IncreaseNonce(c.requestNonce)
	if len(payload) > 0 {
		record = append(record, c.requestAEAD.Seal(nil, c.requestNonce, payload, nil)...)
		snell.IncreaseNonce(c.requestNonce)
	}
	return record
}

func (c *v5RawConn) ReadFirstRecord(t *testing.T) ([]byte, error) {
	salt := make([]byte, snell.SaltLen)
	_, err := io.ReadFull(c.Conn, salt)
	if err != nil {
		return nil, err
	}
	responseAEAD, err := snell.NewAEAD(snell.DeriveKey(c.psk, salt))
	require.NoError(t, err)
	c.responseAEAD = responseAEAD
	c.responseNonce = make([]byte, snell.NonceLen)
	return c.ReadRecord(t)
}

func (c *v5RawConn) ReadRecord(t *testing.T) ([]byte, error) {
	headerCipher := make([]byte, snell.HeaderCipherLen)
	_, err := io.ReadFull(c.Conn, headerCipher)
	if err != nil {
		return nil, err
	}
	header, err := c.responseAEAD.Open(nil, c.responseNonce, headerCipher, nil)
	require.NoError(t, err)
	snell.IncreaseNonce(c.responseNonce)
	require.Len(t, header, snell.HeaderPlainLen)
	require.Equal(t, byte(snell.HeaderVersion), header[0])
	paddingLen := int(binary.BigEndian.Uint16(header[3:5]))
	payloadLen := int(binary.BigEndian.Uint16(header[5:7]))
	if payloadLen == 0 {
		return nil, errV5RawZeroChunk
	}
	padding := make([]byte, paddingLen)
	if paddingLen > 0 {
		_, err = io.ReadFull(c.Conn, padding)
		if err != nil {
			return nil, err
		}
	}
	payloadCipher := make([]byte, payloadLen+snell.AEADTagLen)
	_, err = io.ReadFull(c.Conn, payloadCipher)
	if err != nil {
		return nil, err
	}
	mixLimit := min(len(padding), len(payloadCipher))
	for index := 0; index < mixLimit; index += 2 {
		padding[index], payloadCipher[index] = payloadCipher[index], padding[index]
	}
	payload, err := c.responseAEAD.Open(nil, c.responseNonce, payloadCipher, nil)
	require.NoError(t, err)
	snell.IncreaseNonce(c.responseNonce)
	return payload, nil
}

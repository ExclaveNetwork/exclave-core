package test

import (
	"bytes"
	"crypto/rand"
	"io"
	"net"
	"testing"
	"time"

	snell "github.com/sagernet/sing-snell"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	"github.com/stretchr/testify/require"
)

type normalScenario struct {
	address string
	client  snell.Method
}

func (s normalScenario) HalfCloseEcho(t *testing.T, label string) {
	echoPort := startTCPHalfCloseEcho(t)
	serverConn, err := net.Dial("tcp", s.address)
	require.NoError(t, err)
	defer serverConn.Close()
	proxyConn, err := s.client.DialConn(serverConn, M.ParseSocksaddrHostPort("127.0.0.1", echoPort))
	require.NoError(t, err)
	defer proxyConn.Close()

	payload := make([]byte, 4*1024)
	_, err = rand.Read(payload)
	require.NoError(t, err)
	copy(payload, []byte(label))
	_, err = proxyConn.Write(payload)
	require.NoError(t, err)
	require.NoError(t, N.CloseWrite(proxyConn))
	require.NoError(t, proxyConn.SetReadDeadline(time.Now().Add(10*time.Second)))
	received, err := io.ReadAll(proxyConn)
	require.NoError(t, err)
	require.Equal(t, len(payload), len(received), "%s response length", label)
	require.True(t, bytes.Equal(payload, received), "%s payload mismatch", label)
}

package test

import (
	"io"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func freePort(t *testing.T) uint16 {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := uint16(listener.Addr().(*net.TCPAddr).Port)
	listener.Close()
	return port
}

func startTCPEcho(t *testing.T) uint16 {
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
				io.Copy(conn, conn)
				conn.Close()
			}()
		}
	}()
	return uint16(listener.Addr().(*net.TCPAddr).Port)
}

func startUDPEcho(t *testing.T) uint16 {
	packetConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { packetConn.Close() })
	go func() {
		buffer := make([]byte, 64*1024)
		for {
			n, addr, readErr := packetConn.ReadFrom(buffer)
			if readErr != nil {
				return
			}
			packetConn.WriteTo(buffer[:n], addr)
		}
	}()
	return uint16(packetConn.LocalAddr().(*net.UDPAddr).Port)
}

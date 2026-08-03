package tlsfragment

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"os"
	"slices"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func writeAndWaitAck(ctx context.Context, conn *net.TCPConn, payload []byte, fallbackDelay time.Duration) error {
	start := time.Now()
	if err := writeAndWaitAckInternal(ctx, conn, payload); err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			if _, err := conn.Write(payload); err != nil {
				return err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(fallbackDelay):
				return nil
			}
		}
		return err
	}
	if time.Since(start) <= 20*time.Millisecond {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(fallbackDelay):
			return nil
		}
	}
	return nil
}

func writeAndWaitAckInternal(ctx context.Context, conn *net.TCPConn, payload []byte) error {
	rawConn, err := conn.SyscallConn()
	if err != nil {
		return writeAndWaitAckEStats(ctx, conn, payload)
	}
	var (
		tcpInfo  *tcpInfoV0
		innerErr error
	)
	err = rawConn.Control(func(fd uintptr) {
		tcpInfo, innerErr = getTcpInfo(fd)
	})
	if innerErr != nil || err != nil {
		if err == nil {
			err = innerErr
		} else {
			err = errors.Join(innerErr, err)
		}
	}
	if err != nil {
		if errors.Is(err, windows.WSAEOPNOTSUPP) || errors.Is(err, windows.WSAEINVAL) {
			return writeAndWaitAckEStats(ctx, conn, payload)
		}
		return os.NewSyscallError("WSAIoctl", err)
	}
	bytesOutBefore := tcpInfo.bytesOut
	_, err = conn.Write(payload)
	if err != nil {
		return err
	}
	err = rawConn.Control(func(fd uintptr) {
		for {
			tcpInfo, innerErr = getTcpInfo(fd)
			if innerErr != nil {
				innerErr = os.NewSyscallError("WSAIoctl", innerErr)
				return
			}
			if tcpInfo.bytesOut >= bytesOutBefore+uint64(len(payload)) && tcpInfo.bytesInFlight == 0 {
				return
			}
			select {
			case <-ctx.Done():
				innerErr = ctx.Err()
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	})
	if innerErr != nil || err != nil {
		if err == nil {
			return innerErr
		}
		return errors.Join(innerErr, err)
	}
	return nil
}

func writeAndWaitAckEStats(ctx context.Context, conn *net.TCPConn, payload []byte) error {
	var source, destination netip.AddrPort
	if tcpAddr, ok := conn.LocalAddr().(*net.TCPAddr); ok {
		source = tcpAddr.AddrPort()
	} else {
		return os.ErrInvalid
	}
	if tcpAddr, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
		destination = tcpAddr.AddrPort()
	} else {
		return os.ErrInvalid
	}
	if source.Addr().Is4() {
		tcpTable, err := GetTcpTable()
		if err != nil {
			return err
		}
		rowIndex := slices.IndexFunc(tcpTable, func(row MibTcpRow) bool {
			return source == netip.AddrPortFrom(netip.AddrFrom4(*(*[4]byte)(unsafe.Pointer(&row.DwLocalAddr))), binary.BigEndian.Uint16((*[4]byte)(unsafe.Pointer(&row.DwLocalPort))[:])) &&
				destination == netip.AddrPortFrom(netip.AddrFrom4(*(*[4]byte)(unsafe.Pointer(&row.DwRemoteAddr))), binary.BigEndian.Uint16((*[4]byte)(unsafe.Pointer(&row.DwRemotePort))[:]))
		})
		if rowIndex == -1 {
			rowIndex = slices.IndexFunc(tcpTable, func(row MibTcpRow) bool {
				return source == netip.AddrPortFrom(netip.AddrFrom4(*(*[4]byte)(unsafe.Pointer(&row.DwLocalAddr))), binary.BigEndian.Uint16((*[4]byte)(unsafe.Pointer(&row.DwLocalPort))[:])) ||
					destination == netip.AddrPortFrom(netip.AddrFrom4(*(*[4]byte)(unsafe.Pointer(&row.DwRemoteAddr))), binary.BigEndian.Uint16((*[4]byte)(unsafe.Pointer(&row.DwRemotePort))[:]))
			})
		}
		if rowIndex == -1 {
			return errors.New("row not found for: " + source.String())
		}
		tcpRow := &tcpTable[rowIndex]
		if err := SetPerTcpConnectionEStatsSendBuffer(tcpRow, &TcpEstatsSendBuffRwV0{
			EnableCollection: true,
		}); err != nil {
			return os.NewSyscallError("SetPerTcpConnectionEStatsSendBufferV0", err)
		}
		defer SetPerTcpConnectionEStatsSendBuffer(tcpRow, &TcpEstatsSendBuffRwV0{
			EnableCollection: false,
		})
		if _, err := conn.Write(payload); err != nil {
			return err
		}
		for {
			eStatsSendBuffer, err := GetPerTcpConnectionEStatsSendBuffer(tcpRow)
			if err != nil {
				return err
			}
			if eStatsSendBuffer.CurRetxQueue == 0 {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(10 * time.Millisecond):
			}
		}
	} else {
		tcpTable, err := GetTcp6Table()
		if err != nil {
			return err
		}
		rowIndex := slices.IndexFunc(tcpTable, func(row MibTcp6Row) bool {
			return source == netip.AddrPortFrom(netip.AddrFrom16(row.LocalAddr), binary.BigEndian.Uint16((*[4]byte)(unsafe.Pointer(&row.LocalPort))[:])) &&
				destination == netip.AddrPortFrom(netip.AddrFrom16(row.RemoteAddr), binary.BigEndian.Uint16((*[4]byte)(unsafe.Pointer(&row.RemotePort))[:]))
		})
		if rowIndex == -1 {
			rowIndex = slices.IndexFunc(tcpTable, func(row MibTcp6Row) bool {
				return source == netip.AddrPortFrom(netip.AddrFrom16(row.LocalAddr), binary.BigEndian.Uint16((*[4]byte)(unsafe.Pointer(&row.LocalPort))[:])) ||
					destination == netip.AddrPortFrom(netip.AddrFrom16(row.RemoteAddr), binary.BigEndian.Uint16((*[4]byte)(unsafe.Pointer(&row.RemotePort))[:]))
			})
		}
		if rowIndex == -1 {
			return errors.New("row not found for: " + source.String())
		}
		tcpRow := &tcpTable[rowIndex]
		if err := SetPerTcp6ConnectionEStatsSendBuffer(tcpRow, &TcpEstatsSendBuffRwV0{
			EnableCollection: true,
		}); err != nil {
			return os.NewSyscallError("SetPerTcpConnectionEStatsSendBufferV0", err)
		}
		defer SetPerTcp6ConnectionEStatsSendBuffer(tcpRow, &TcpEstatsSendBuffRwV0{
			EnableCollection: false,
		})
		if _, err := conn.Write(payload); err != nil {
			return err
		}
		for {
			eStatsSendBuffer, err := GetPerTcp6ConnectionEStatsSendBuffer(tcpRow)
			if err != nil {
				return err
			}
			if eStatsSendBuffer.CurRetxQueue == 0 {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
}

package tlsfragment

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

const sioTcpInfo = 0xD8000027

type tcpInfoV0 struct {
	state             uint32
	mss               uint32
	connectionTimeMs  uint64
	timestampsEnabled uint8
	rttUs             uint32
	minRttUs          uint32
	bytesInFlight     uint32
	cwnd              uint32
	sndWnd            uint32
	rcvWnd            uint32
	rcvBuf            uint32
	bytesOut          uint64
	bytesIn           uint64
	bytesReordered    uint32
	bytesRetrans      uint32
	fastRetrans       uint32
	dupAcksIn         uint32
	timeoutEpisodes   uint32
	synRetrans        uint8
}

func getTcpInfo(fd uintptr) (*tcpInfoV0, error) {
	version := uint32(0)
	var tcpInfo tcpInfoV0
	var bytesReturned uint32
	err := windows.WSAIoctl(
		windows.Handle(fd),
		sioTcpInfo,
		(*byte)(unsafe.Pointer(&version)),
		uint32(unsafe.Sizeof(version)),
		(*byte)(unsafe.Pointer(&tcpInfo)),
		uint32(unsafe.Sizeof(tcpInfo)),
		&bytesReturned,
		nil,
		0,
	)
	if err != nil {
		return nil, err
	}
	return &tcpInfo, nil
}

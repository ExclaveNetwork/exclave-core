package snellv4

import (
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
)

// Stubs for sing APIs newer than v0.8.11. Methods that create these always
// return unsupported (nil, false) on this branch of exclave-core.

type PacketBatchWriter interface {
	WritePacketBatch(buffers []*buf.Buffer, destinations []M.Socksaddr) error
}

type PacketBatchWriteCreator interface {
	CreatePacketBatchWriter() (PacketBatchWriter, bool)
}

type ConnectedPacketBatchWriteCreator interface {
	CreateConnectedPacketBatchWriter() (PacketBatchWriter, bool)
}

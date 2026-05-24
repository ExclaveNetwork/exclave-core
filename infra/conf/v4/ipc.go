package v4

import (
	"github.com/golang/protobuf/proto"

	"github.com/exclavenetwork/exclave-core/v5/proxy/ipc"
)

type IPCConfig struct {
	Level uint32 `json:"level"`
}

func (c *IPCConfig) Build() (proto.Message, error) {
	return &ipc.ServerConfig{
		Level: int32(c.Level),
	}, nil
}

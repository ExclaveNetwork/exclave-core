package v4

import (
	"github.com/golang/protobuf/proto"

	"github.com/exclavenetwork/exclave-core/v5/infra/conf/cfgcommon"
	"github.com/exclavenetwork/exclave-core/v5/proxy/snell"
)

type SnellClientConfig struct {
	Address *cfgcommon.Address `json:"address"`
	Port    uint16             `json:"port"`
	PSK     string             `json:"psk"`
	Obfs    string             `json:"obfs"`
	Version uint32             `json:"version"`
	Reuse   bool               `json:"reuse"`
}

func (c *SnellClientConfig) Build() (proto.Message, error) {
	if c.Address == nil {
		return nil, newError("missing server address")
	}
	if c.PSK == "" {
		return nil, newError("missing psk")
	}
	version := c.Version
	if version == 0 {
		version = 4
	}
	return &snell.ClientConfig{
		Address: c.Address.Build(),
		Port:    uint32(c.Port),
		Psk:     c.PSK,
		Obfs:    c.Obfs,
		Version: version,
		Reuse:   c.Reuse,
	}, nil
}

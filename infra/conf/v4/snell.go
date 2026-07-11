package v4

import (
	"github.com/golang/protobuf/proto"

	"github.com/exclavenetwork/exclave-core/v5/infra/conf/cfgcommon"
	"github.com/exclavenetwork/exclave-core/v5/proxy/snell"
)

type SnellClientConfig struct {
	Address  *cfgcommon.Address `json:"address"`
	Port     uint16             `json:"port"`
	PSK      string             `json:"psk"`
	UserKey  string             `json:"userKey"`
	Version  uint32             `json:"version"`
	Reuse    bool               `json:"reuse"`
	ObfsMode string             `json:"obfsMode"`
	ObfsHost string             `json:"obfsHost"`
	Mode     string             `json:"mode"`
}

func (c *SnellClientConfig) Build() (proto.Message, error) {
	if c.Address == nil {
		return nil, newError("missing server address")
	}
	return &snell.ClientConfig{
		Address:  c.Address.Build(),
		Port:     uint32(c.Port),
		Psk:      c.PSK,
		UserKey:  c.UserKey,
		Version:  c.Version,
		Reuse:    c.Reuse,
		ObfsMode: c.ObfsMode,
		ObfsHost: c.ObfsHost,
		Mode:     c.Mode,
	}, nil
}

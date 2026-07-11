package v4

import (
	"strings"

	"github.com/golang/protobuf/proto"

	"github.com/exclavenetwork/exclave-core/v5/infra/conf/cfgcommon"
	"github.com/exclavenetwork/exclave-core/v5/proxy/snell"
)

type SnellClientConfig struct {
	Address        *cfgcommon.Address `json:"address"`
	Port           uint16             `json:"port"`
	PSK            string             `json:"psk"`
	UserKey        string             `json:"userKey"`
	Version        uint32             `json:"version"`
	Reuse          bool               `json:"reuse"`
	ObfsMode       string             `json:"obfsMode"`
	ObfsHost       string             `json:"obfsHost"`
	Mode           string             `json:"mode"`
	DomainStrategy string             `json:"domainStrategy"`
}

func (c *SnellClientConfig) Build() (proto.Message, error) {
	if c.Address == nil {
		return nil, newError("missing server address")
	}
	config := &snell.ClientConfig{
		Address:  c.Address.Build(),
		Port:     uint32(c.Port),
		Psk:      c.PSK,
		UserKey:  c.UserKey,
		Version:  c.Version,
		Reuse:    c.Reuse,
		ObfsMode: c.ObfsMode,
		ObfsHost: c.ObfsHost,
		Mode:     c.Mode,
	}
	switch strings.ToLower(c.DomainStrategy) {
	case "useip", "":
		config.DomainStrategy = snell.ClientConfig_USE_IP
	case "useipv4":
		config.DomainStrategy = snell.ClientConfig_USE_IP4
	case "useipv6":
		config.DomainStrategy = snell.ClientConfig_USE_IP6
	case "preferipv4":
		config.DomainStrategy = snell.ClientConfig_PREFER_IP4
	case "preferipv6":
		config.DomainStrategy = snell.ClientConfig_PREFER_IP6
	default:
		return nil, newError("unsupported domain strategy: ", c.DomainStrategy)
	}
	return config, nil
}

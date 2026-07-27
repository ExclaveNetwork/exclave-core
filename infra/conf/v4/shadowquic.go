package v4

import (
	"github.com/golang/protobuf/proto"

	"github.com/exclavenetwork/exclave-core/v5/infra/conf/cfgcommon"
	"github.com/exclavenetwork/exclave-core/v5/proxy/shadowquic"
)

type ShadowQUICClientConfig struct {
	Address           *cfgcommon.Address `json:"address"`
	Port              uint16             `json:"port"`
	Username          string             `json:"username"`
	Password          string             `json:"password"`
	CongestionControl string             `json:"congestionControl"`
	UDPOverStream     bool               `json:"udpOverStream"`
	ZeroRTTHandshake  bool               `json:"zeroRTTHandshake"`
	ServerName        string             `json:"serverName"`
	ALPN              []string           `json:"alpn"`
}

func (c *ShadowQUICClientConfig) Build() (proto.Message, error) {
	if c.Address == nil {
		return nil, newError("missing server address")
	}
	return &shadowquic.ClientConfig{
		Address:           c.Address.Build(),
		Port:              uint32(c.Port),
		Username:          c.Username,
		Password:          c.Password,
		CongestionControl: c.CongestionControl,
		UdpOverStream:     c.UDPOverStream,
		ZeroRttHandshake:  c.ZeroRTTHandshake,
		ServerName:        c.ServerName,
		Alpn:              c.ALPN,
	}, nil
}

package simplified

import (
	"context"

	"github.com/exclavenetwork/exclave-core/v5/common"
	"github.com/exclavenetwork/exclave-core/v5/proxy/mixed"
)

func init() {
	common.Must(common.RegisterConfig((*ServerConfig)(nil), func(ctx context.Context, config interface{}) (interface{}, error) {
		simplifiedServer := config.(*ServerConfig)
		fullServer := &mixed.ServerConfig{
			AuthType:   mixed.AuthType_NO_AUTH,
			Address:    simplifiedServer.Address,
			UdpEnabled: simplifiedServer.UdpEnabled,
		}
		return common.CreateObject(ctx, fullServer)
	}))
}

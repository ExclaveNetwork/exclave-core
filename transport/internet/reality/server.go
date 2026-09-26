package reality

import (
	"context"

	"github.com/exclavenetwork/reality"

	"github.com/exclavenetwork/exclave-core/v5/common/net"
)

func Server(ctx context.Context, conn net.Conn, config *reality.Config) (net.Conn, error) {
	realityConn, err := reality.RealityServer(ctx, conn, config)
	if err != nil {
		// conn closed by reality.RealityServer
		return nil, err
	}
	return &Conn{Conn: realityConn}, nil
}

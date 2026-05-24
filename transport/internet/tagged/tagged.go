package tagged

import (
	"context"

	"github.com/exclavenetwork/exclave-core/v5/common/net"
)

type DialFunc func(ctx context.Context, dest net.Destination, tag string) (net.Conn, error)

var Dialer DialFunc

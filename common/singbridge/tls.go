package singbridge

import (
	"context"
	"crypto/tls"
	"time"

	singtls "github.com/sagernet/sing/common/tls"

	"github.com/exclavenetwork/exclave-core/v5/common/net"
	v2tls "github.com/exclavenetwork/exclave-core/v5/transport/internet/tls"
)

var _ singtls.Config = (*tlsConfigWrapper)(nil)

func NewTLSConfigWrapper(ctx context.Context, config *v2tls.Config, opts ...v2tls.Option) *tlsConfigWrapper {
	return &tlsConfigWrapper{
		ctx:    ctx,
		config: config.Clone(),
		opts:   opts,
	}
}

type tlsConfigWrapper struct {
	ctx    context.Context
	config *v2tls.Config
	opts   []v2tls.Option
}

func (c *tlsConfigWrapper) ServerName() string {
	// placeholder
	return c.config.GetTLSConfig(c.opts...).ServerName
}

func (c *tlsConfigWrapper) SetServerName(serverName string) {
	// placeholder
	c.config.ServerName = serverName
}

func (c *tlsConfigWrapper) NextProtos() []string {
	// placeholder
	return c.config.GetTLSConfig(c.opts...).NextProtos
}

func (c *tlsConfigWrapper) SetNextProtos(nextProtos []string) {
	// placeholder
	c.config.NextProtocol = nextProtos
}

func (c *tlsConfigWrapper) STDConfig() (*tls.Config, error) {
	return c.config.GetTLSConfigWithContext(c.ctx, c.opts...)
}

func (c *tlsConfigWrapper) Client(conn net.Conn) (singtls.Conn, error) {
	// placeholder
	stdConfig, err := c.STDConfig()
	if err != nil {
		return nil, err
	}
	return tls.Client(conn, stdConfig), nil
}

func (c *tlsConfigWrapper) HandshakeTimeout() time.Duration {
	// placeholder
	return -1
}

func (c *tlsConfigWrapper) SetHandshakeTimeout(_ time.Duration) {
	// placeholder
}

func (c *tlsConfigWrapper) Clone() singtls.Config {
	// placeholder
	return &tlsConfigWrapper{
		ctx:    c.ctx,
		config: c.config.Clone(),
		opts:   c.opts,
	}
}

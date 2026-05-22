package singbridge

import (
	"context"
	"crypto/tls"

	singtls "github.com/sagernet/sing/common/tls"

	"github.com/v2fly/v2ray-core/v5/common/net"
	v2tls "github.com/v2fly/v2ray-core/v5/transport/internet/tls"
)

var _ singtls.Config = (*tlsConfigWrapper)(nil)

func NewTLSConfigWrapper(ctx context.Context, config *v2tls.Config, opts ...v2tls.Option) *tlsConfigWrapper {
	return &tlsConfigWrapper{
		ctx:    ctx,
		config: config,
		opts:   opts,
	}
}

type tlsConfigWrapper struct {
	ctx    context.Context
	config *v2tls.Config
	opts   []v2tls.Option
}

func (c *tlsConfigWrapper) ServerName() string {
	panic("invalid")
}

func (c *tlsConfigWrapper) SetServerName(_ string) {
	panic("invalid")
}

func (c *tlsConfigWrapper) NextProtos() []string {
	panic("invalid")
}

func (c *tlsConfigWrapper) SetNextProtos(_ []string) {
	panic("invalid")
}

func (c *tlsConfigWrapper) STDConfig() (*tls.Config, error) {
	return c.config.GetTLSConfigWithContext(c.ctx, c.opts...)
}

func (c *tlsConfigWrapper) Client(_ net.Conn) (singtls.Conn, error) {
	panic("invalid")
}

func (c *tlsConfigWrapper) Clone() singtls.Config {
	panic("invalid")
}

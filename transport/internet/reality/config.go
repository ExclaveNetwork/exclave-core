package reality

import (
	"context"
	"net"
	"time"

	"github.com/exclavenetwork/reality"
	"github.com/pires/go-proxyproto"

	"github.com/exclavenetwork/exclave-core/v5/transport/internet"
)

type option func(*reality.Config)

func WithNextProto(alpn ...string) option {
	return func(config *reality.Config) {
		config.NextProtos = alpn
	}
}

type Conn struct {
	*reality.Conn
}

func (c *Config) GetREALITYConfig() *reality.Config {
	var dialer net.Dialer
	config := &reality.Config{
		SessionTicketsDisabled: true,
		NextProtos:             nil, // should be nil
		RealityServerConfig: reality.RealityServerConfig{
			PrivateKey:  c.PrivateKey,
			MLDSA65Seed: c.Mldsa65Seed,
			ServerNames: make(map[string]struct{}),
			MaxTimeDiff: time.Duration(c.MaxTimeDiff) * time.Millisecond,
			DialContext: func(ctx context.Context) (net.Conn, error) {
				return dialer.DialContext(ctx, c.Type, c.Dest)
			},
		},
	}
	for _, serverName := range c.ServerNames {
		config.RealityServerConfig.ServerNames[serverName] = struct{}{}
	}
	if len(c.ShortIds) > 0 {
		config.RealityServerConfig.ShortIds = map[[8]byte]struct{}{}
		for _, shortId := range c.ShortIds {
			config.RealityServerConfig.ShortIds[[8]byte(shortId)] = struct{}{}
		}
	}
	if c.Xver == 1 || c.Xver == 2 {
		config.RealityServerConfig.WriteProxyProtoHeader = func(source, dest net.Addr, conn net.Conn) (int64, error) {
			header := proxyproto.HeaderProxyFromAddrs(byte(c.Xver), source, dest)
			return header.WriteTo(conn)
		}
		config.RealityServerConfig.UnwrapProxyProtoConn = func(conn net.Conn) (net.Conn, bool) {
			if proxyprotoConn, ok := conn.(*proxyproto.Conn); ok {
				return proxyprotoConn.Raw(), true
			}
			return nil, false
		}
	}
	return config
}

func ConfigFromStreamSettings(settings *internet.MemoryStreamConfig) *Config {
	if settings == nil {
		return nil
	}
	config, ok := settings.SecuritySettings.(*Config)
	if !ok {
		return nil
	}
	return config
}

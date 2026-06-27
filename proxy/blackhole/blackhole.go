// Package blackhole is an outbound handler that blocks all connections.
package blackhole

import (
	"context"
	"time"

	"github.com/exclavenetwork/exclave-core/v5/common"
	"github.com/exclavenetwork/exclave-core/v5/common/signal/done"
	"github.com/exclavenetwork/exclave-core/v5/transport"
	"github.com/exclavenetwork/exclave-core/v5/transport/internet"
)

// Handler is an outbound connection that silently swallow the entire payload.
type Handler struct {
	response ResponseConfig
	done     *done.Instance
}

// New creates a new blackhole handler.
func New(ctx context.Context, config *Config) (*Handler, error) {
	response, err := config.GetInternalResponse()
	if err != nil {
		return nil, err
	}
	return &Handler{
		response: response,
		done:     done.New(),
	}, nil
}

// Process implements OutboundHandler.Dispatch().
func (h *Handler) Process(ctx context.Context, link *transport.Link, dialer internet.Dialer) error {
	nBytes := h.response.WriteTo(link.Writer)
	if nBytes > 0 {
		// Sleep a little here to make sure the response is sent to client.
		select {
		case <-ctx.Done():
		case <-h.done.Wait():
		case <-time.After(time.Second):
		}
	}
	return nil
}

func (h *Handler) Close() error {
	h.done.Close()
	return nil
}

func init() {
	common.Must(common.RegisterConfig((*Config)(nil), func(ctx context.Context, config interface{}) (interface{}, error) {
		return New(ctx, config.(*Config))
	}))

	common.Must(common.RegisterConfig((*SimplifiedConfig)(nil), func(ctx context.Context, config interface{}) (interface{}, error) {
		simplifiedServer := config.(*SimplifiedConfig)
		_ = simplifiedServer
		fullConfig := &Config{}
		return common.CreateObject(ctx, fullConfig)
	}))
}

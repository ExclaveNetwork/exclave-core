package juicity

import (
	"context"

	"github.com/exclavenetwork/exclave-core/v5/common"
)

func init() {
	common.Must(common.RegisterConfig((*ServerConfig)(nil), func(ctx context.Context, config interface{}) (interface{}, error) {
		return NewServer(ctx, config.(*ServerConfig))
	}))
}

type Inbound struct {
}

func NewServer(ctx context.Context, config *ServerConfig) (*Inbound, error) {
	return &Inbound{}, nil
}

func (i *Inbound) Close() error {
	return nil
}

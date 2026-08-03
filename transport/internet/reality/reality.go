package reality

import (
	"context"

	"github.com/exclavenetwork/exclave-core/v5/common"
)

//go:generate go run github.com/exclavenetwork/exclave-core/v5/common/errors/errorgen

func init() {
	common.Must(common.RegisterConfig((*Config)(nil), func(ctx context.Context, config interface{}) (interface{}, error) {
		return config, nil
	}))
}

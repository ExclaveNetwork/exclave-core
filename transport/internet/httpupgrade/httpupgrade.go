package httpupgrade

import (
	"github.com/exclavenetwork/exclave-core/v5/common"
	"github.com/exclavenetwork/exclave-core/v5/transport/internet"
)

//go:generate go run github.com/exclavenetwork/exclave-core/v5/common/errors/errorgen

const protocolName = "httpupgrade"

func init() {
	common.Must(internet.RegisterProtocolConfigCreator(protocolName, func() interface{} {
		return new(Config)
	}))
}

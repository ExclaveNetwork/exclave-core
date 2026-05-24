package all

import (
	"github.com/exclavenetwork/exclave-core/v5/main/commands/all/api"
	"github.com/exclavenetwork/exclave-core/v5/main/commands/all/tls"
	"github.com/exclavenetwork/exclave-core/v5/main/commands/base"
)

func init() {
	base.RootCommand.Commands = append(
		base.RootCommand.Commands,
		api.CmdAPI,
		cmdLove,
		tls.CmdTLS,
		cmdUUID,
		cmdX25519,
		cmdMLKEM768,
		cmdWG,

		// documents
		docFormat,
		docMerge,
	)
}

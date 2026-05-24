package environment

import "github.com/exclavenetwork/exclave-core/v5/common/log"

type ConnectionCapabilitySet interface {
	ConnectionLogCapabilitySet
}

type ConnectionEnvironment interface {
	ConnectionCapabilitySet
	doNotImpl()
}

type ConnectionLogCapabilitySet interface {
	RecordConnectionLog(msg log.Message)
}

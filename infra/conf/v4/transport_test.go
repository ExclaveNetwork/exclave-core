package v4_test

import (
	"encoding/json"
	"testing"

	"github.com/golang/protobuf/proto"

	"github.com/exclavenetwork/exclave-core/v5/infra/conf/cfgcommon/socketcfg"
	"github.com/exclavenetwork/exclave-core/v5/infra/conf/cfgcommon/testassist"
	"github.com/exclavenetwork/exclave-core/v5/transport/internet"
)

func TestSocketConfig(t *testing.T) {
	createParser := func() func(string) (proto.Message, error) {
		return func(s string) (proto.Message, error) {
			config := new(socketcfg.SocketConfig)
			if err := json.Unmarshal([]byte(s), config); err != nil {
				return nil, err
			}
			return config.Build()
		}
	}

	testassist.RunMultiTestCase(t, []testassist.TestCase{
		{
			Input: `{
				"mark": 1,
				"tcpFastOpen": true,
				"tcpFastOpenQueueLength": 1024,
				"mptcp": true
			}`,
			Parser: createParser(),
			Output: &internet.SocketConfig{
				Mark:           1,
				Tfo:            internet.SocketConfig_Enable,
				TfoQueueLength: 1024,
				Mptcp:          internet.MPTCPState_Enable,
			},
		},
	})
}

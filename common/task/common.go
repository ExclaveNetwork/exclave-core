package task

import "github.com/exclavenetwork/exclave-core/v5/common"

// Close returns a func() that closes v.
func Close(v interface{}) func() error {
	return func() error {
		return common.Close(v)
	}
}

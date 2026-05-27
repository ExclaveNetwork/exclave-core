package track

import (
	"container/list"
	"sync"
	"time"
	// "github.com/exclavenetwork/exclave-core/v5/common"
)

type ConnectionPool struct {
	list.List
	sync.Mutex
}

func NewConnectionPool() *ConnectionPool {
	return new(ConnectionPool)
}

func (p *ConnectionPool) ResetConnections() {
	now := time.Now()
	p.Lock()
	for elem := p.Front(); elem != nil; elem = elem.Next() {
		// common.Close(elem.Value)
		// Use `SetDeadline` instead of `Close` to avoid double close
		if setDeadlineFn, ok := elem.Value.(interface {
			SetDeadline(time.Time) error
		}); ok {
			setDeadlineFn.SetDeadline(now)
		}
	}
	p.Init()
	p.Unlock()
}

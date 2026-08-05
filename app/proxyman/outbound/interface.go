package outbound

import (
	"container/list"
	"sync"
)

var (
	interfaceUpdateCallbackMutex sync.Mutex
	interfaceUpdateCallBackList  list.List
)

func RegisterInterfaceUpdateCallback(callback func()) *list.Element {
	interfaceUpdateCallbackMutex.Lock()
	elem := interfaceUpdateCallBackList.PushBack(callback)
	interfaceUpdateCallbackMutex.Unlock()
	return elem
}

func UnRegisterInterfaceUpdateCallback(elem *list.Element) {
	interfaceUpdateCallbackMutex.Lock()
	interfaceUpdateCallBackList.Remove(elem)
	interfaceUpdateCallbackMutex.Unlock()
}

func InterfaceUpdate() {
	interfaceUpdateCallbackMutex.Lock()
	callbacks := make([]func(), 0, interfaceUpdateCallBackList.Len())
	for elem := interfaceUpdateCallBackList.Front(); elem != nil; elem = elem.Next() {
		callbacks = append(callbacks, elem.Value.(func()))
	}
	interfaceUpdateCallbackMutex.Unlock()
	for _, callback := range callbacks {
		callback()
	}
}

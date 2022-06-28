package wasm

import "fmt"

type CallFrame struct {
	Pc          uint64
	FunctionIdx uint32 // function index
}

type Snapshot struct {
	Valid   bool
	Stack   []uint64
	Globals []*GlobalInstance
	Frames  []CallFrame
}

func (snap *Snapshot) String() string {
	return fmt.Sprintf("Call Frame: %v, Stack: %v, Globals: %v", snap.Frames, snap.Stack, snap.Globals)
	//return ""
}

func (frame CallFrame) String() string {
	return fmt.Sprintf("Fn %d@%d", frame.FunctionIdx, frame.Pc)
}

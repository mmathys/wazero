package wasm

import "fmt"

type Snapshot struct {
	Valid   bool
	Pc      uint64
	Stack   []uint64
	Globals []*GlobalInstance
}

func (snap *Snapshot) Format() string {
	return fmt.Sprintf("Pc: %d, Stack: %v, Globals: %v", snap.Pc, snap.Stack, snap.Globals)
}

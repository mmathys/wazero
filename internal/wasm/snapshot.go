package wasm

type Snapshot struct {
	Valid     bool
	SomeField string
	Params    []uint64
	Pc        uint64
	Stack     []uint64
}

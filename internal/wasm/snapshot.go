package wasm

type Snapshot struct {
	Valid bool
	Pc    uint64
	Stack []uint64
}

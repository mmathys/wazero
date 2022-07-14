package wasm

import (
	"fmt"

	"github.com/tetratelabs/wazero/internal/sys"
)

type CallFrame struct {
	Pc          uint64
	FunctionIdx uint32 // function index
}

type Snapshot struct {
	Valid   bool
	Stack   []uint64
	Globals []*GlobalInstance
	Frames  []CallFrame
	Memory  *MemoryInstance

	// file system
	LastFD      uint32
	OpenedFiles map[uint32]*sys.FileEntry
}

func (snap *Snapshot) String() string {
	return fmt.Sprintf("Call Frame: %v, Stack: %v, Globals: %v, LastFD: %v", snap.Frames, snap.Stack, snap.Globals, snap.LastFD)
}

func (frame CallFrame) String() string {
	return fmt.Sprintf("Fn %d@%d", frame.FunctionIdx, frame.Pc)
}

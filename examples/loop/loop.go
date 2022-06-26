package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/internal/wasm"
	"github.com/tetratelabs/wazero/internal/wasmruntime"
)

// stackWasm was generated by the following:
//	cd testdata; wat2wasm --debug-names add.wat
//go:embed testdata/stack.wasm
var stackWasm []byte

// main implements a basic function in both Go and WebAssembly.
func main() {
	// Choose the context to use for function calls.
	ctx := context.Background()
	snapshot := &wasm.Snapshot{}
	ctx = context.WithValue(ctx, "snapshot", snapshot)

	r := wazero.NewRuntimeWithConfig(wazero.NewRuntimeConfigInterpreter())
	defer r.Close(ctx) // This closes everything this Runtime created.

	module, err := r.InstantiateModuleFromBinary(ctx, stackWasm)
	if err != nil {
		log.Panicln(err)
	}

	// Read two args to add.
	x := readArg()

loop:
	for {
		add := module.ExportedFunction("add3").(*wasm.FunctionInstance)
		var results []uint64
		var err error
		if snapshot.Valid {
			results, err = add.Resume(ctx, snapshot)
		} else {
			results, err = add.Call(ctx, x)
		}
		switch err {
		case wasmruntime.ErrRuntimeSnapshot:
			log.Printf("new snapshot: %v\n", snapshot)
		case nil:
			fmt.Printf("%s: %d + 3 = %d\n", module.Name(), x, results[0])
			break loop
		default:
			log.Panicln(err)
		}
	}

}

func readArg() uint64 {
	x, err := strconv.ParseUint(os.Args[1], 10, 64)
	if err != nil {
		log.Panicf("invalid arg %v: %v", os.Args[1], err)
	}

	return x
}

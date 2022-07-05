package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"io/ioutil"
	"log"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/internal/proto"
	"github.com/tetratelabs/wazero/internal/wasm"
	"github.com/tetratelabs/wazero/internal/wasmruntime"
	pb "google.golang.org/protobuf/proto"
)

// stackWasm was generated by the following:
//	cd testdata; wat2wasm --debug-names add.wat
//go:embed testdata/fib.wasm
var stackWasm []byte

func main() {
	ctx := context.Background()

	snapshot := &wasm.Snapshot{}
	ctx = context.WithValue(ctx, "snapshot", snapshot)

	alwaysSnapshot := flag.Bool("always-snapshot", false, "snapshot after every wasm instruction")
	traceOnly := flag.Bool("trace", false, "trace execution, do not trap")
	haltAfterSnapshot := flag.Bool("halt", false, "halt execution after snapshot")
	exportSnapshot := flag.Bool("export", false, "export the snapshot to snapshot.bin")
	snapshotFile := flag.String("from-snapshot", "none", "path to resume execution from a snapshot binary file")
	flag.Parse()

	ctx = context.WithValue(ctx, "always_snapshot", *traceOnly || *alwaysSnapshot)
	ctx = context.WithValue(ctx, "trap_after_snapshot", !*traceOnly)
	ctx = context.WithValue(ctx, "export_snapshot", *exportSnapshot)

	r := wazero.NewRuntimeWithConfig(wazero.NewRuntimeConfigInterpreter())
	defer r.Close(ctx) // This closes everything this Runtime created.

	// Read arg
	var x uint64 = 6
	var y uint64 = 2

	// if snapshotFile is defined, read it and update the snapshot struct.
	if *snapshotFile != "none" {
		readSnapshot(*snapshotFile, snapshot)
	}

loop:
	for {
		// load module
		module, err := r.InstantiateModuleFromBinary(ctx, stackWasm)
		if err != nil {
			log.Panicln(err)
		}

		// load function
		add := module.ExportedFunction("entry").(*wasm.FunctionInstance)

		// execute
		var results []uint64
		if snapshot.Valid {
			results, err = add.Resume(ctx, snapshot)
		} else {
			results, err = add.Call(ctx, x, y)
		}

		// close module
		module.Close(ctx)

		// print iteration
		switch err {
		case wasmruntime.ErrRuntimeSnapshot:
			//log.Printf("snapshot: %v\n", snapshot)
		case nil:
			fmt.Printf("result: %d\n", results[0])
			break loop
		default:
			log.Panicln(err)
		}

		if *haltAfterSnapshot {
			break loop
		}
	}

}

func readSnapshot(snapshotFile string, res *wasm.Snapshot) {
	log.Println("reading snapshot")
	in, err := ioutil.ReadFile(snapshotFile)
	if err != nil {
		log.Fatalln("Error reading file:", err)
	}
	snapshotPb := &proto.Snapshot{}
	if err := pb.Unmarshal(in, snapshotPb); err != nil {
		log.Fatalln("Failed to parse snapshot:", err)
	}

	res.Valid = true
	res.Stack = snapshotPb.GetStack()

	// Globals
	res.Globals = nil
	for _, global := range snapshotPb.GetGlobals() {
		globalInstance := &wasm.GlobalInstance{
			Val:   global.GetValue(),
			ValHi: global.GetValHi(),
		}
		globalType := &wasm.GlobalType{
			Mutable: global.GetMutable(),
		}
		switch global.Type {
		case proto.ValueType_I32:
			globalType.ValType = wasm.ValueTypeI32
		case proto.ValueType_I64:
			globalType.ValType = wasm.ValueTypeI64
		case proto.ValueType_F32:
			globalType.ValType = wasm.ValueTypeF32
		case proto.ValueType_F64:
			globalType.ValType = wasm.ValueTypeF64
		case proto.ValueType_V128:
			globalType.ValType = wasm.ValueTypeV128
		case proto.ValueType_FuncRef:
			globalType.ValType = wasm.ValueTypeFuncref
		case proto.ValueType_ExternRef:
			globalType.ValType = wasm.ValueTypeExternref
		}
		globalInstance.Type = globalType
		res.Globals = append(res.Globals, globalInstance)
	}

	res.Frames = nil
	for _, frame := range snapshotPb.GetFrames() {
		callFrame := wasm.CallFrame{
			Pc:          frame.Pc,
			FunctionIdx: frame.FunctionIndex,
		}
		res.Frames = append(res.Frames, callFrame)
	}

	res.Memory = &wasm.MemoryInstance{
		Buffer: snapshotPb.GetMemory().GetBuffer(),
		Min:    snapshotPb.GetMemory().GetMin(),
		Max:    snapshotPb.GetMemory().GetMax(),
		Cap:    snapshotPb.GetMemory().GetCap(),
	}
}

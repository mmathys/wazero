package main

import (
	"context"
	"embed"
	_ "embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/internal/wasm"
	"github.com/tetratelabs/wazero/internal/wasmruntime"
	"github.com/tetratelabs/wazero/wasi_snapshot_preview1"
)

// catFS is an embedded filesystem limited to test.txt
//go:embed testdata/test.txt
var catFS embed.FS

// catWasmTinyGo was compiled from testdata/tinygo/cat.go
//go:embed testdata/copy.wasm
var testWasm []byte

// main writes an input file to stdout, just like `cat`.
//
// This is a basic introduction to the WebAssembly System Interface (WASI).
// See https://github.com/WebAssembly/WASI
func main() {
	// Choose the context to use for function calls.
	ctx := context.Background()

	snapshot := &wasm.Snapshot{}
	ctx = context.WithValue(ctx, "snapshot", snapshot)

	alwaysSnapshot := flag.Bool("always-snapshot", false, "snapshot after every wasm instruction")
	traceOnly := flag.Bool("trace", false, "trace execution, do not trap")
	//haltAfterSnapshot := flag.Bool("halt", false, "halt execution after snapshot")
	exportSnapshot := flag.Bool("export", false, "export the snapshot to snapshot.bin")
	snapshotFile := flag.String("from-snapshot", "none", "path to resume execution from a snapshot binary file")
	flag.Parse()

	ctx = context.WithValue(ctx, "always_snapshot", *traceOnly || *alwaysSnapshot)
	ctx = context.WithValue(ctx, "trap_after_snapshot", !*traceOnly)
	ctx = context.WithValue(ctx, "export_snapshot", *exportSnapshot)

	// Create a new WebAssembly Runtime.
	r := wazero.NewRuntimeWithConfig(wazero.NewRuntimeConfigInterpreter().
		// Enable WebAssembly 2.0 support, which is required for TinyGo 0.24+.
		WithWasmCore2())
	defer r.Close(ctx) // This closes everything this Runtime created.

	// Since wazero uses fs.FS, we can use standard libraries to do things like trim the leading path.
	rooted, err := fs.Sub(catFS, "testdata")
	if err != nil {
		log.Panicln(err)
	}

	// if snapshotFile is defined, read it and update the snapshot struct.
	if *snapshotFile != "none" {
		readSnapshot(*snapshotFile, snapshot)
	}

	// Combine the above into our baseline config, overriding defaults.
	config := wazero.NewModuleConfig().
		// By default, I/O streams are discarded and there's no file system.
		WithStdout(os.Stdout).WithStderr(os.Stderr).WithFS(rooted)

	// Instantiate WASI, which implements system I/O such as console output.
	if _, err = wasi_snapshot_preview1.Instantiate(ctx, r); err != nil {
		log.Panicln(err)
	}

	// Compile the WebAssembly module using the default configuration.
	code, err := r.CompileModule(ctx, testWasm, wazero.NewCompileConfig())
	if err != nil {
		log.Panicln(err)
	}

loop:
	for {
		modCtx := context.WithValue(ctx, "trap_after_snapshot", false)
		modCtx = context.WithValue(modCtx, "always_snapshot", false)
		module, err := r.InstantiateModule(modCtx, code, config.WithArgs(("wasi")))
		if err != nil {
			log.Panicln(err)
		}

		entry := module.ExportedFunction("entry").(*wasm.FunctionInstance)
		if err != nil {
			log.Panicln(err)
		}

		var results []uint64
		if snapshot.Valid {
			results, err = entry.Resume(ctx, snapshot)
		} else {
			results, err = entry.Call(ctx)
		}

		// print iteration
		switch err {
		case wasmruntime.ErrRuntimeSnapshot:
			log.Printf("snapshot: %v\n", snapshot)
		case nil:
			fmt.Printf("result: %d\n", results[0])
			break loop
		default:
			log.Panicln(err)
		}

		module.Close(ctx)
	}
}

func readSnapshot(snapshotFile string, res *wasm.Snapshot) {
	// not implemented.
}

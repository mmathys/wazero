package wasm_exec_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/tetratelabs/wazero/internal/testing/require"
	"github.com/tetratelabs/wazero/internal/wasm"
	"github.com/tetratelabs/wazero/internal/wasm/binary"
)

// Test_compileJsWasm ensures the infrastructure to generate wasm on-demand works.
func Test_compileJsWasm(t *testing.T) {
	bin := compileJsWasm(t, "basic", `package main

import "os"

func main() {
	os.Exit(1)
}`)

	m, err := binary.DecodeModule(bin, wasm.Features20191205, wasm.MemorySizer)
	require.NoError(t, err)
	// TODO: implement go.buildid custom section and validate it instead.
	require.NotNil(t, m.MemorySection)
}

// compileJsWasm allows us to generate a binary with runtime.GOOS=js and runtime.GOARCH=wasm.
func compileJsWasm(t *testing.T, name string, mainSrc string) []byte {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	goBin := requireGoBin(t)
	workDir := t.TempDir()
	bin := name + ".wasm"
	goArgs := []string{"build", "-o", bin, "."}
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "main.go"), []byte(mainSrc), 0o600))

	require.NoError(t, os.WriteFile(filepath.Join(workDir, "go.mod"),
		[]byte("module github.com/tetratelabs/wazero/wasm_exec/examples\n\ngo 1.17\n"), 0o600))

	cmd := exec.CommandContext(ctx, goBin, goArgs...) //nolint:gosec
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "couldn't compile %s: %s", bin, string(out))
	bytes, err := os.ReadFile(filepath.Join(workDir, bin)) //nolint:gosec
	require.NoError(t, err)
	return bytes
}

func requireGoBin(t *testing.T) string {
	binName := "go"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", binName)
	if _, err := os.Stat(goBin); err == nil {
		return goBin
	}
	// Now, search the path
	goBin, err := exec.LookPath(binName)
	if err != nil {
		t.Skip("skipping (probably CI is running scratch)")
	}
	return goBin
}

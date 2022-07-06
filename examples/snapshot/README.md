# Snapshot examples

## Transform Wat to Wasm

Use `wat2wasm` from [WABT](https://github.com/WebAssembly/wabt)

```bash
wat2wasm file.wat file.wasm
```

## Compile c files to Wasm

```bash
./compile.sh call.c call.wasm # or fib.c
wasm2wat call.wasm # to inspect generated wasm
```

## Test snapshots of wasm files

Change the line `//go:embed testdata/<file>.wasm` to the wasm program you want to test.

```bash
go run snapshot.go # just calculate the result
go run snapshot.go -trace # for tracing only
go run snapshot.go -always-snapshot # snapshot after every instruction
go run snapshot.go -halt # halt after first snapshot
```
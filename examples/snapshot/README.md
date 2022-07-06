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
go run snapshot.go -export # export snapshot as binary
go run snapshot.go -from-snapshot=snapshot.bin # resume from snapshot 
```

## E2E snapshot functionality example

1. Compile the fibonacci example
 
```bash
./compile.sh fib.c fib.wasm
```

2. Transform wasm to wat for inspection

```bash
wasm2wat fib.wasm > fib.wat
```

3. Add a `nop` instruction to signal a breakpoint for snapshotting (anywhere, e.g. line 14)

4. Transform wat back to wasm

```bash
wat2wasm fib.wat
```

5. Change the line `//go:embed testdata/fib.wasm`.

6. Export the first snapshot to `snapshot.bin`

```bash
go run snapshot.go -export -halt
```

7. Read snapshot, execute until next snapshot, and export it again.
Run this command again until the result is outputted.

```bash
go run snapshot.go -export -halt -from-snapshot=snapshot.bin
```

8. Compare the result to the version with no snapshots.

```bash
go run snapshot.go -trace
```

The flag `-trace` only prints the snapshots, but does not actually trap.


# Snapshot examples

## Transform Wat to Wasm

Use `wat2wasm` from [WABT](https://github.com/WebAssembly/wabt)

```bash
wat2wasm file.wat file.wasm
```

## Compile c files to Wasm

```bash
./compile.sh call.c # or fib.c
```

`wasm2wat` is useful to inspect the generated wasm.

## Test snapshots of wasm files

If `TrapAfterSnapshot = true`, then a execution breaks after making a snapshot.
`TrapAfterSnapshot = false` is only used for tracing the execution by printing snapshots.
It won't actually halt the execution.

Option 1: use flags `AlwaysSnapshot = true`. This triggers a snapshot after every wasm instruction.

Option 2: manually insert `nop` instructions by hand. Runtime is snapshotting when seeing a `nop` instruction.
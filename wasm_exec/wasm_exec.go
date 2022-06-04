// Package wasm_exec contains imports and state needed by wasm go compiles when
// GOOS=js and GOARCH=wasm.
//
// See /wasm_exec/REFERENCE.md for a deeper dive.
package wasm_exec

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	internalsys "github.com/tetratelabs/wazero/internal/sys"
	"github.com/tetratelabs/wazero/internal/wasm"
	"github.com/tetratelabs/wazero/sys"
)

// Instantiate instantiates the "go" imports used by wasm_exec into the runtime
// default namespace.
//
// Notes
//
//	* Closing the wazero.Runtime has the same effect as closing the result.
//	* To instantiate into another wazero.Namespace, use NewBuilder instead.
func Instantiate(ctx context.Context, r wazero.Runtime) (api.Closer, error) {
	return NewBuilder(r).Instantiate(ctx, r)
}

// Builder configures the "go" imports used by wasm_exec.js for later use via
// Compile or Instantiate.
type Builder interface {

	// Compile compiles the "go" module that can instantiated in any namespace
	// (wazero.Namespace).
	//
	// Note: This has the same effect as this function on wazero.ModuleBuilder.
	Compile(context.Context, wazero.CompileConfig) (wazero.CompiledModule, error)

	// Instantiate instantiates the "go" module into the provided namespace.
	//
	// Note: This has the same effect as this function on wazero.ModuleBuilder.
	Instantiate(context.Context, wazero.Namespace) (api.Closer, error)
}

// NewBuilder returns a new Builder.
func NewBuilder(r wazero.Runtime) Builder {
	return &builder{r: r}
}

type builder struct {
	r wazero.Runtime
}

// moduleBuilder returns a new wazero.ModuleBuilder
func (b *builder) moduleBuilder() wazero.ModuleBuilder {
	g := &jsWasm{}
	return b.r.NewModuleBuilder("go").
		ExportFunction("runtime.wasmExit", g._wasmExit).
		ExportFunction("runtime.wasmWrite", g._wasmWrite).
		ExportFunction("runtime.resetMemoryDataView", g._resetMemoryDataView).
		ExportFunction("runtime.nanotime1", g._nanotime1).
		ExportFunction("runtime.walltime", g._walltime).
		ExportFunction("runtime.scheduleTimeoutEvent", g._scheduleTimeoutEvent).
		ExportFunction("runtime.clearTimeoutEvent", g._clearTimeoutEvent).
		ExportFunction("runtime.getRandomData", g._getRandomData).
		ExportFunction("syscall/js.finalizeRef", g._finalizeRef).
		ExportFunction("syscall/js.stringVal", g._stringVal).
		ExportFunction("syscall/js.valueGet", g._valueGet).
		ExportFunction("syscall/js.valueSet", g._valueSet).
		ExportFunction("syscall/js.valueDelete", g._valueDelete).
		ExportFunction("syscall/js.valueIndex", g._valueIndex).
		ExportFunction("syscall/js.valueSetIndex", g._valueSetIndex).
		ExportFunction("syscall/js.valueCall", g._valueCall).
		ExportFunction("syscall/js.valueInvoke", g._valueInvoke).
		ExportFunction("syscall/js.valueNew", g._valueNew).
		ExportFunction("syscall/js.valueLength", g._valueLength).
		ExportFunction("syscall/js.valuePrepareString", g._valuePrepareString).
		ExportFunction("syscall/js.valueLoadString", g._valueLoadString).
		ExportFunction("syscall/js.valueInstanceOf", g._valueInstanceOf).
		ExportFunction("syscall/js.copyBytesToGo", g._copyBytesToGo).
		ExportFunction("syscall/js.copyBytesToJS", g._copyBytesToJS).
		ExportFunction("debug", g.debug)
}

// Compile implements Builder.Compile
func (b *builder) Compile(ctx context.Context, config wazero.CompileConfig) (wazero.CompiledModule, error) {
	return b.moduleBuilder().Compile(ctx, config)
}

// Instantiate implements Builder.Instantiate
func (b *builder) Instantiate(ctx context.Context, ns wazero.Namespace) (api.Closer, error) {
	return b.moduleBuilder().Instantiate(ctx, ns)
}

// jsWasm holds defines the "go" imports used by wasm_exec.
//
// Note: This is module-scoped, so only safe when used in a wazero.Namespace
// that only instantiates one module.
type jsWasm struct {
	mux                   sync.RWMutex
	nextCallbackTimeoutID uint32                 // guarded by mux
	scheduledTimeouts     map[uint32]*time.Timer // guarded by mux

	closed *uint64
}

// debug has unknown use, so stubbed.
//
// See https://github.com/golang/go/blob/go1.19beta1/src/cmd/link/internal/wasm/asm.go#L133-L138
func (j *jsWasm) debug(ctx context.Context, mod api.Module, sp uint32) {
}

// _wasmExit converts the GOARCH=wasm stack to be compatible with api.ValueType
// in order to call wasmExit.
func (j *jsWasm) _wasmExit(ctx context.Context, mod api.Module, sp uint32) {
	code := requireReadUint32Le(ctx, mod.Memory(), "code", sp+8)
	j.wasmExit(ctx, mod, code)
}

// wasmExit implements runtime.wasmExit which supports runtime.exit.
//
// See https://github.com/golang/go/blob/go1.19beta1/src/runtime/sys_wasm.go#L28
func (j *jsWasm) wasmExit(ctx context.Context, mod api.Module, code uint32) {
	closed := uint64(1) + uint64(code)<<32 // Store exitCode as high-order bits.
	if !atomic.CompareAndSwapUint64(j.closed, 0, closed) {
		return
	}

	// TODO: free resources for this module
	_ = mod.CloseWithExitCode(ctx, code)
}

// _wasmWrite converts the GOARCH=wasm stack to be compatible with
// api.ValueType in order to call wasmWrite.
func (j *jsWasm) _wasmWrite(ctx context.Context, mod api.Module, sp uint32) {
	fd := requireReadUint64Le(ctx, mod.Memory(), "fd", sp+8)
	p := requireReadUint64Le(ctx, mod.Memory(), "p", sp+16)
	n := requireReadUint32Le(ctx, mod.Memory(), "n", sp+24)
	j.wasmWrite(ctx, mod, fd, p, n)
}

// wasmWrite implements runtime.wasmWrite which supports runtime.write and
// runtime.writeErr. It is only known to be used with fd = 2 (stderr).
//
// See https://github.com/golang/go/blob/go1.19beta1/src/runtime/os_js.go#L29
func (j *jsWasm) wasmWrite(ctx context.Context, mod api.Module, fd, p uint64, n uint32) {
	var writer io.Writer

	switch fd {
	case 1:
		writer = getSysCtx(mod).Stdout()
	case 2:
		writer = getSysCtx(mod).Stderr()
	default:
		// Keep things simple by expecting nothing past 2
		panic(fmt.Errorf("unexpected fd %d", fd))
	}

	if _, err := writer.Write(requireRead(ctx, mod.Memory(), "p", uint32(p), n)); err != nil {
		panic(fmt.Errorf("error writing p: %w", err))
	}
}

// _resetMemoryDataView converts the GOARCH=wasm stack to be compatible with
// api.ValueType in order to call resetMemoryDataView.
func (j *jsWasm) _resetMemoryDataView(ctx context.Context, mod api.Module, sp uint32) {
	j.resetMemoryDataView(ctx, mod)
}

// resetMemoryDataView signals wasm.OpcodeMemoryGrow happened, indicating any
// cached view of memory should be reset.
//
// See https://github.com/golang/go/blob/9839668b5619f45e293dd40339bf0ac614ea6bee/src/runtime/mem_js.go#L82
func (j *jsWasm) resetMemoryDataView(ctx context.Context, mod api.Module) {
	// TODO: Compiler-based memory.grow callbacks are ignored until we have a generic solution #601
}

// _nanotime1 converts the GOARCH=wasm stack to be compatible with
// api.ValueType in order to call nanotime1.
func (j *jsWasm) _nanotime1(ctx context.Context, mod api.Module, sp uint32) {
	nanos := j.nanotime1(ctx, mod)
	requireWriteUint64Le(ctx, mod.Memory(), "t", sp+8, nanos)
}

// nanotime1 implements runtime.nanotime which supports time.Since.
//
// See https://github.com/golang/go/blob/go1.19beta1/src/runtime/sys_wasm.s#L184
func (j *jsWasm) nanotime1(ctx context.Context, mod api.Module) uint64 {
	return uint64(getSysCtx(mod).Nanotime(ctx))
}

// _walltime converts the GOARCH=wasm stack to be compatible with
// api.ValueType in order to call walltime.
func (j *jsWasm) _walltime(ctx context.Context, mod api.Module, sp uint32) {
	sec, nsec := j.walltime(ctx, mod)
	requireWriteUint64Le(ctx, mod.Memory(), "sec", sp+8, uint64(sec))
	requireWriteUint32Le(ctx, mod.Memory(), "nsec", sp+16, uint32(nsec))
}

// walltime implements runtime.walltime which supports time.Now.
//
// See https://github.com/golang/go/blob/go1.19beta1/src/runtime/sys_wasm.s#L188
func (j *jsWasm) walltime(ctx context.Context, mod api.Module) (uint64 int64, uint32 int32) {
	return getSysCtx(mod).Walltime(ctx)
}

// _scheduleTimeoutEvent converts the GOARCH=wasm stack to be compatible with
// api.ValueType in order to call scheduleTimeoutEvent.
func (j *jsWasm) _scheduleTimeoutEvent(ctx context.Context, mod api.Module, sp uint32) {
	delayMs := requireReadUint64Le(ctx, mod.Memory(), "delay", sp+8)
	id := j.scheduleTimeoutEvent(ctx, mod, delayMs)
	requireWriteUint32Le(ctx, mod.Memory(), "id", sp+16, id)
}

// scheduleTimeoutEvent implements runtime.scheduleTimeoutEvent which supports
// runtime.notetsleepg used by runtime.signal_recv.
//
// Unlike other most functions prefixed by "runtime.", this both launches a
// goroutine and invokes code compiled into wasm "resume".
//
// See https://github.com/golang/go/blob/go1.19beta1/src/runtime/sys_wasm.s#L192
func (j *jsWasm) scheduleTimeoutEvent(ctx context.Context, mod api.Module, delayMs uint64) uint32 {
	delay := time.Duration(delayMs) * time.Millisecond

	resume := mod.ExportedFunction("resume")

	// Invoke resume as an anonymous function, to propagate the context.
	callResume := func() {
		if err := j.failIfClosed(mod); err != nil {
			return
		}
		// While there's a possible error here, panicking won't help as it is
		// on a different goroutine.
		_, _ = resume.Call(ctx)
	}

	return j.scheduleEvent(delay, callResume)
}

// scheduleEvent schedules an event onto another goroutine after d duration and
// returns a handle to remove it (removeEvent).
func (j *jsWasm) scheduleEvent(d time.Duration, f func()) uint32 {
	j.mux.Lock()
	defer j.mux.Unlock()

	id := j.nextCallbackTimeoutID
	j.nextCallbackTimeoutID++
	// TODO: this breaks the sandbox (proc.checkTimers is shared), so should
	// be substitutable with a different impl.
	j.scheduledTimeouts[id] = time.AfterFunc(d, f)
	return id
}

// _clearTimeoutEvent converts the GOARCH=wasm stack to be compatible with
// api.ValueType in order to call clearTimeoutEvent.
func (j *jsWasm) _clearTimeoutEvent(ctx context.Context, mod api.Module, sp uint32) {
	id := requireReadUint32Le(ctx, mod.Memory(), "id", sp+8)
	j.clearTimeoutEvent(id)
}

// clearTimeoutEvent implements runtime.clearTimeoutEvent which supports
// runtime.notetsleepg used by runtime.signal_recv.
//
// See https://github.com/golang/go/blob/go1.19beta1/src/runtime/sys_wasm.s#L196
func (j *jsWasm) clearTimeoutEvent(id uint32) {
	if t := j.removeEvent(id); t != nil {
		if !t.Stop() {
			<-t.C
		}
	}
}

// removeEvent removes an event previously scheduled with scheduleEvent or
// returns nil, if it was already removed.
func (j *jsWasm) removeEvent(id uint32) *time.Timer {
	j.mux.Lock()
	defer j.mux.Unlock()

	t, ok := j.scheduledTimeouts[id]
	if ok {
		delete(j.scheduledTimeouts, id)
		return t
	}
	return nil
}

// _getRandomData converts the GOARCH=wasm stack to be compatible with
// api.ValueType in order to call getRandomData.
func (j *jsWasm) _getRandomData(ctx context.Context, mod api.Module, sp uint32) {
	buf := uint32(requireReadUint64Le(ctx, mod.Memory(), "buf", sp+8))
	bufLen := uint32(requireReadUint64Le(ctx, mod.Memory(), "bufLen", sp+16))

	j.getRandomData(ctx, mod, buf, bufLen)
}

// getRandomData implements runtime.getRandomData, which initializes the seed
// for runtime.fastrand.
//
// See https://github.com/golang/go/blob/go1.19beta1/src/runtime/sys_wasm.s#L200
func (j *jsWasm) getRandomData(ctx context.Context, mod api.Module, buf, bufLen uint32) {
	randSource := getSysCtx(mod).RandSource()

	r := requireRead(ctx, mod.Memory(), "r", buf, bufLen)

	if n, err := randSource.Read(r); err != nil {
		panic(fmt.Errorf("RandSource.Read(r /* len =%d */) failed: %w", bufLen, err))
	} else if n != int(bufLen) {
		panic(fmt.Errorf("RandSource.Read(r /* len =%d */) read %d bytes", bufLen, n))
	}
}

// _finalizeRef converts the GOARCH=wasm stack to be compatible with
// api.ValueType in order to call finalizeRef.
func (j *jsWasm) _finalizeRef(ctx context.Context, mod api.Module, sp uint32) {
	r := requireReadUint32Le(ctx, mod.Memory(), "r", sp+8)
	j.finalizeRef(ctx, mod, r)
}

// finalizeRef implements js.finalizeRef, which is used as a
// runtime.SetFinalizer on the given reference.
//
// See https://github.com/golang/go/blob/go1.19beta1/src/syscall/js/js.go#L61
func (j *jsWasm) finalizeRef(ctx context.Context, mod api.Module, r uint32) {
	panic(fmt.Errorf("TODO: finalizeRef(%d)", r))
}

// _stringVal converts the GOARCH=wasm stack to be compatible with
// api.ValueType in order to call stringVal.
func (j *jsWasm) _stringVal(ctx context.Context, mod api.Module, sp uint32) {
	xAddr := requireReadUint64Le(ctx, mod.Memory(), "xAddr", sp+8)
	xLen := requireReadUint64Le(ctx, mod.Memory(), "xLen", sp+16)
	xRef := j.stringVal(ctx, mod, xAddr, xLen)
	requireWriteUint64Le(ctx, mod.Memory(), "xRef", sp+24, xRef)
}

// stringVal implements js.stringVal, which is used to load the string for
// `js.ValueOf(x)`.
//
// See https://github.com/golang/go/blob/go1.19beta1/src/syscall/js/js.go#L212
//     https://github.com/golang/go/blob/go1.19beta1/misc/wasm/wasm_exec.js#L305-L308
func (j *jsWasm) stringVal(ctx context.Context, mod api.Module, xAddr, xLen uint64) uint64 {
	x := requireRead(ctx, mod.Memory(), "x", uint32(xAddr), uint32(xLen))
	return j.valueRef(ctx, mod, x)
}

// _valueGet converts the GOARCH=wasm stack to be compatible with
// api.ValueType in order to call valueGet.
func (j *jsWasm) _valueGet(ctx context.Context, mod api.Module, sp uint32) {
	vRef := requireReadUint64Le(ctx, mod.Memory(), "vRef", sp+8)
	pAddr := requireReadUint64Le(ctx, mod.Memory(), "pAddr", sp+16)
	pLen := requireReadUint64Le(ctx, mod.Memory(), "pLen", sp+24)
	xRef := j.valueGet(ctx, mod, vRef, pAddr, pLen)
	sp = j.refreshSP(ctx, mod)
	requireWriteUint64Le(ctx, mod.Memory(), "xRef", sp+32, xRef)
}

// valueGet implements js.valueGet, which is used to load a js.Value property
// by name, ex. `v.Get("address")`.
//
// See https://github.com/golang/go/blob/go1.19beta1/src/syscall/js/js.go#L295
//     https://github.com/golang/go/blob/go1.19beta1/misc/wasm/wasm_exec.js#L311-L316
func (j *jsWasm) valueGet(ctx context.Context, mod api.Module, vRef, pAddr, pLen uint64) uint64 {
	v := j.loadValue(ctx, mod, uint32(vRef))
	p := requireRead(ctx, mod.Memory(), "p", uint32(pAddr), uint32(pLen))
	result := j.reflectGet(v, string(p))
	return j.valueRef(ctx, mod, result)
}

// _valueSet converts the GOARCH=wasm stack to be compatible with
// api.ValueType in order to call valueSet.
func (j *jsWasm) _valueSet(ctx context.Context, mod api.Module, sp uint32) {
	vRef := requireReadUint64Le(ctx, mod.Memory(), "vRef", sp+8)
	pAddr := requireReadUint64Le(ctx, mod.Memory(), "pAddr", sp+16)
	pLen := requireReadUint64Le(ctx, mod.Memory(), "pLen", sp+24)
	xRef := requireReadUint64Le(ctx, mod.Memory(), "xRef", sp+32)
	j.valueSet(ctx, mod, vRef, pAddr, pLen, xRef)
}

// valueSet implements js.valueSet, which is used to store a js.Value property
// by name, ex. `v.Set("address", a)`.
//
// See https://github.com/golang/go/blob/go1.19beta1/src/syscall/js/js.go#L309
//     https://github.com/golang/go/blob/go1.19beta1/misc/wasm/wasm_exec.js#L318-L322
func (j *jsWasm) valueSet(ctx context.Context, mod api.Module, vRef, pAddr, pLen, xRef uint64) {
	v := j.loadValue(ctx, mod, uint32(vRef))
	p := requireRead(ctx, mod.Memory(), "p", uint32(pAddr), uint32(pLen))
	x := j.loadValue(ctx, mod, uint32(xRef))
	j.reflectSet(v, string(p), x)
}

// _valueDelete converts the GOARCH=wasm stack to be compatible with
// api.ValueType in order to call valueDelete.
func (j *jsWasm) _valueDelete(ctx context.Context, mod api.Module, sp uint32) {
	vRef := requireReadUint64Le(ctx, mod.Memory(), "vRef", sp+8)
	pAddr := requireReadUint64Le(ctx, mod.Memory(), "pAddr", sp+16)
	pLen := requireReadUint64Le(ctx, mod.Memory(), "pLen", sp+24)
	j.valueDelete(ctx, mod, vRef, pAddr, pLen)
}

// valueDelete implements js.valueDelete, which is used to delete a js.Value property
// by name, ex. `v.Delete("address")`.
//
// See https://github.com/golang/go/blob/go1.19beta1/src/syscall/js/js.go#L321
//     https://github.com/golang/go/blob/go1.19beta1/misc/wasm/wasm_exec.js#L325-L328
func (j *jsWasm) valueDelete(ctx context.Context, mod api.Module, vRef, pAddr, pLen uint64) {
	v := j.loadValue(ctx, mod, uint32(vRef))
	p := requireRead(ctx, mod.Memory(), "p", uint32(pAddr), uint32(pLen))
	j.reflectDeleteProperty(v, string(p))
}

// _valueIndex converts the GOARCH=wasm stack to be compatible with
// api.ValueType in order to call valueIndex.
func (j *jsWasm) _valueIndex(ctx context.Context, mod api.Module, sp uint32) {
	vRef := requireReadUint64Le(ctx, mod.Memory(), "vRef", sp+8)
	i := requireReadUint64Le(ctx, mod.Memory(), "i", sp+16)
	xRef := j.valueIndex(ctx, mod, vRef, i)
	sp = j.refreshSP(ctx, mod)
	requireWriteUint64Le(ctx, mod.Memory(), "xRef", sp+32, xRef)
}

// valueIndex implements js.valueIndex, which is used to load a js.Value property
// by name, ex. `v.Index(0)`.
//
// See https://github.com/golang/go/blob/go1.19beta1/src/syscall/js/js.go#L334
//     https://github.com/golang/go/blob/go1.19beta1/misc/wasm/wasm_exec.js#L331-L334
func (j *jsWasm) valueIndex(ctx context.Context, mod api.Module, vRef, i uint64) uint64 {
	v := j.loadValue(ctx, mod, uint32(vRef))
	result := j.reflectGetIndex(v, int(i))
	return j.valueRef(ctx, mod, result)
}

// _valueSetIndex converts the GOARCH=wasm stack to be compatible with
// api.ValueType in order to call valueSetIndex.
func (j *jsWasm) _valueSetIndex(ctx context.Context, mod api.Module, sp uint32) {
	vRef := requireReadUint64Le(ctx, mod.Memory(), "vRef", sp+8)
	i := requireReadUint64Le(ctx, mod.Memory(), "i", sp+16)
	xRef := requireReadUint64Le(ctx, mod.Memory(), "xRef", sp+24)
	j.valueSetIndex(ctx, mod, vRef, i, xRef)
}

// valueSetIndex implements js.valueSetIndex, which is used to store a js.Value property
// by name, ex. `v.SetIndex(0, a)`.
//
// See https://github.com/golang/go/blob/go1.19beta1/src/syscall/js/js.go#L348
//     https://github.com/golang/go/blob/go1.19beta1/misc/wasm/wasm_exec.js#L337-L340
func (j *jsWasm) valueSetIndex(ctx context.Context, mod api.Module, vRef, i, xRef uint64) {
	v := j.loadValue(ctx, mod, uint32(vRef))
	x := j.loadValue(ctx, mod, uint32(xRef))
	j.reflectSetIndex(v, int(i), x)
}

// _valueCall converts the GOARCH=wasm stack to be compatible with
// api.ValueType in order to call valueCall.
func (j *jsWasm) _valueCall(ctx context.Context, mod api.Module, sp uint32) {
	vRef := requireReadUint64Le(ctx, mod.Memory(), "vRef", sp+8)
	mAddr := requireReadUint64Le(ctx, mod.Memory(), "mAddr", sp+16)
	mLen := requireReadUint64Le(ctx, mod.Memory(), "mLen", sp+24)
	argsArray := requireReadUint64Le(ctx, mod.Memory(), "argsArray", sp+32)
	argsLen := requireReadUint64Le(ctx, mod.Memory(), "argsLen", sp+40)
	xRef, ok := j.valueCall(ctx, mod, vRef, mAddr, mLen, argsArray, argsLen)
	sp = j.refreshSP(ctx, mod)
	requireWriteUint64Le(ctx, mod.Memory(), "xRef", sp+56, xRef)
	requireWriteByte(ctx, mod.Memory(), "ok", sp+64, byte(ok))
}

// valueCall implements js.valueCall, which is used to call a js.Value function
// by name, ex. `document.Call("createElement", "div")`.
//
// See https://github.com/golang/go/blob/go1.19beta1/src/syscall/js/js.go#L394
//     https://github.com/golang/go/blob/go1.19beta1/misc/wasm/wasm_exec.js#L343-L358
func (j *jsWasm) valueCall(ctx context.Context, mod api.Module, vRef, mAddr, mLen, argsArray, argsLen uint64) (uint64, uint32) {
	v := j.loadValue(ctx, mod, uint32(vRef))
	m := requireRead(ctx, mod.Memory(), "m", uint32(mAddr), uint32(mLen))
	args := j.loadSliceOfValues(ctx, mod, uint32(argsArray), uint32(argsLen))
	if result, err := j.reflectApply(m, v, args); err != nil {
		return j.valueRef(ctx, mod, err), 0
	} else {
		return j.valueRef(ctx, mod, result), 1
	}
}

// _valueInvoke converts the GOARCH=wasm stack to be compatible with
// api.ValueType in order to call valueInvoke.
func (j *jsWasm) _valueInvoke(ctx context.Context, mod api.Module, sp uint32) {
	vRef := requireReadUint64Le(ctx, mod.Memory(), "vRef", sp+8)
	argsArray := requireReadUint64Le(ctx, mod.Memory(), "argsArray", sp+16)
	argsLen := requireReadUint64Le(ctx, mod.Memory(), "argsLen", sp+24)
	xRef, ok := j.valueInvoke(ctx, mod, vRef, argsArray, argsLen)
	sp = j.refreshSP(ctx, mod)
	requireWriteUint64Le(ctx, mod.Memory(), "xRef", sp+40, xRef)
	requireWriteByte(ctx, mod.Memory(), "ok", sp+48, byte(ok))
}

// valueInvoke implements js.valueInvoke, which is used to call a js.Value, ex.
// `add.Invoke(1, 2)`.
//
// See https://github.com/golang/go/blob/go1.19beta1/src/syscall/js/js.go#L413
//     https://github.com/golang/go/blob/go1.19beta1/misc/wasm/wasm_exec.js#L361-L375
func (j *jsWasm) valueInvoke(ctx context.Context, mod api.Module, vRef, argsArray, argsLen uint64) (uint64, uint32) {
	v := j.loadValue(ctx, mod, uint32(vRef))
	args := j.loadSliceOfValues(ctx, mod, uint32(argsArray), uint32(argsLen))
	if result, err := j.reflectApply(v, nil, args); err != nil {
		return j.valueRef(ctx, mod, err), 0
	} else {
		return j.valueRef(ctx, mod, result), 1
	}
}

// _valueNew converts the GOARCH=wasm stack to be compatible with
// api.ValueType in order to call valueNew.
func (j *jsWasm) _valueNew(ctx context.Context, mod api.Module, sp uint32) {
	vRef := requireReadUint64Le(ctx, mod.Memory(), "vRef", sp+8)
	argsArray := requireReadUint64Le(ctx, mod.Memory(), "argsArray", sp+16)
	argsLen := requireReadUint64Le(ctx, mod.Memory(), "argsLen", sp+24)
	xRef, ok := j.valueNew(ctx, mod, vRef, argsArray, argsLen)
	sp = j.refreshSP(ctx, mod)
	requireWriteUint64Le(ctx, mod.Memory(), "xRef", sp+40, xRef)
	requireWriteByte(ctx, mod.Memory(), "ok", sp+48, byte(ok))
}

// valueNew implements js.valueNew, which is used to call a js.Value, ex.
// `array.New(2)`.
//
// See https://github.com/golang/go/blob/go1.19beta1/src/syscall/js/js.go#L432
//     https://github.com/golang/go/blob/go1.19beta1/misc/wasm/wasm_exec.js#L380-L391
func (j *jsWasm) valueNew(ctx context.Context, mod api.Module, vRef, argsArray, argsLen uint64) (uint64, uint32) {
	v := j.loadValue(ctx, mod, uint32(vRef))
	args := j.loadSliceOfValues(ctx, mod, uint32(argsArray), uint32(argsLen))
	if result, err := j.reflectConstruct(v, args); err != nil {
		return j.valueRef(ctx, mod, err), 0
	} else {
		return j.valueRef(ctx, mod, result), 1
	}
}

// _valueLength converts the GOARCH=wasm stack to be compatible with
// api.ValueType in order to call valueLength.
func (j *jsWasm) _valueLength(ctx context.Context, mod api.Module, sp uint32) {
	vRef := requireReadUint64Le(ctx, mod.Memory(), "vRef", sp+8)
	length := j.valueLength(ctx, mod, vRef)
	requireWriteUint64Le(ctx, mod.Memory(), "length", sp+16, length)
}

// valueLength implements js.valueLength, which is used to load the length
// property of a value, ex. `array.length`.
//
// See https://github.com/golang/go/blob/go1.19beta1/src/syscall/js/js.go#L372
//     https://github.com/golang/go/blob/go1.19beta1/misc/wasm/wasm_exec.js#L396-L397
func (j *jsWasm) valueLength(ctx context.Context, mod api.Module, vRef uint64) uint64 {
	panic(fmt.Errorf("TODO: valueLength(%d)", vRef))
}

// _valuePrepareString converts the GOARCH=wasm stack to be compatible with
// api.ValueType in order to call valuePrepareString.
func (j *jsWasm) _valuePrepareString(ctx context.Context, mod api.Module, sp uint32) {
	vRef := requireReadUint64Le(ctx, mod.Memory(), "vRef", sp+8)
	sAddr, sLen := j.valuePrepareString(ctx, mod, vRef)
	requireWriteUint64Le(ctx, mod.Memory(), "sAddr", sp+16, sAddr)
	requireWriteUint64Le(ctx, mod.Memory(), "sLen", sp+24, sLen)
}

// valuePrepareString implements js.valuePrepareString, which is used to load
// the string for `obj.String()` (via js.jsString) for string, boolean and
// number types.
//
// See https://github.com/golang/go/blob/go1.19beta1/src/syscall/js/js.go#L531
//     https://github.com/golang/go/blob/go1.19beta1/misc/wasm/wasm_exec.js#L402-L405
func (j *jsWasm) valuePrepareString(ctx context.Context, mod api.Module, vRef uint64) (uint64, uint64) {
	v := j.loadValue(ctx, mod, uint32(vRef))
	s := j.valueString(v)
	return j.valueRef(ctx, mod, s), uint64(len(s))
}

// _valueLoadString converts the GOARCH=wasm stack to be compatible with
// api.ValueType in order to call valueLoadString.
func (j *jsWasm) _valueLoadString(ctx context.Context, mod api.Module, sp uint32) {
	vRef := requireReadUint64Le(ctx, mod.Memory(), "vRef", sp+8)
	bAddr := requireReadUint64Le(ctx, mod.Memory(), "bAddr", sp+16)
	bLen := requireReadUint64Le(ctx, mod.Memory(), "bLen", sp+24)
	j.valueLoadString(ctx, mod, vRef, bAddr, bLen)
}

// valueLoadString implements js.valueLoadString, which is used copy a string
// value for `obj.String()`.
//
// See https://github.com/golang/go/blob/go1.19beta1/src/syscall/js/js.go#L533
//     https://github.com/golang/go/blob/go1.19beta1/misc/wasm/wasm_exec.js#L410-L412
func (j *jsWasm) valueLoadString(ctx context.Context, mod api.Module, vRef, bAddr, bLen uint64) {
	v := j.loadValue(ctx, mod, uint32(vRef))
	s := j.valueString(v)
	b := requireRead(ctx, mod.Memory(), "b", uint32(bAddr), uint32(bLen))
	copy(b, s)
}

// _valueInstanceOf converts the GOARCH=wasm stack to be compatible with
// api.ValueType in order to call valueInstanceOf.
func (j *jsWasm) _valueInstanceOf(ctx context.Context, mod api.Module, sp uint32) {
	vRef := requireReadUint64Le(ctx, mod.Memory(), "vRef", sp+8)
	tRef := requireReadUint64Le(ctx, mod.Memory(), "tRef", sp+16)
	r := j.valueInstanceOf(ctx, mod, vRef, tRef)
	requireWriteByte(ctx, mod.Memory(), "r", sp+24, byte(r))
}

// valueInstanceOf implements js.valueInstanceOf. ex. `array instanceof String`.
//
// See https://github.com/golang/go/blob/go1.19beta1/src/syscall/js/js.go#L543
//     https://github.com/golang/go/blob/go1.19beta1/misc/wasm/wasm_exec.js#L417-L418
func (j *jsWasm) valueInstanceOf(ctx context.Context, mod api.Module, vRef, tRef uint64) uint32 {
	v := j.loadValue(ctx, mod, uint32(vRef))
	t := j.loadValue(ctx, mod, uint32(tRef))
	if j.instanceOf(v, t) {
		return 0
	}
	return 1
}

// _copyBytesToGo converts the GOARCH=wasm stack to be compatible with
// api.ValueType in order to call copyBytesToGo.
func (j *jsWasm) _copyBytesToGo(ctx context.Context, mod api.Module, sp uint32) {
	dstAddr := requireReadUint64Le(ctx, mod.Memory(), "dstAddr", sp+8)
	dstLen := requireReadUint64Le(ctx, mod.Memory(), "dstLen", sp+16)
	srcRef := requireReadUint64Le(ctx, mod.Memory(), "srcRef", sp+32)
	n, ok := j.copyBytesToGo(ctx, mod, dstAddr, dstLen, srcRef)
	requireWriteUint64Le(ctx, mod.Memory(), "n", sp+40, n)
	requireWriteByte(ctx, mod.Memory(), "ok", sp+48, byte(ok))
}

type uint8Array []byte // nolint

// copyBytesToGo implements js.copyBytesToGo.
//
// Results
//
//	* n is the count of bytes written.
//	* ok is false if the src was not a uint8Array.
//
// See https://github.com/golang/go/blob/go1.19beta1/src/syscall/js/js.go#L569
//     https://github.com/golang/go/blob/go1.19beta1/misc/wasm/wasm_exec.js#L424-L433
func (j *jsWasm) copyBytesToGo(ctx context.Context, mod api.Module, dstAddr, dstLen, srcRef uint64) (n uint64, ok uint32) {
	dst := requireRead(ctx, mod.Memory(), "dst", uint32(dstAddr), uint32(dstLen)) // nolint
	v := j.loadValue(ctx, mod, uint32(srcRef))
	if src, ok := v.(uint8Array); ok {
		return uint64(copy(dst, src)), 1
	}
	return 0, 0
}

// _copyBytesToJS converts the GOARCH=wasm stack to be compatible with
// api.ValueType in order to call copyBytesToJS.
func (j *jsWasm) _copyBytesToJS(ctx context.Context, mod api.Module, sp uint32) {
	dstRef := requireReadUint64Le(ctx, mod.Memory(), "dstRef", sp+8)
	srcAddr := requireReadUint64Le(ctx, mod.Memory(), "srcAddr", sp+16)
	srcLen := requireReadUint64Le(ctx, mod.Memory(), "srcLen", sp+24)
	n, ok := j.copyBytesToJS(ctx, mod, dstRef, srcAddr, srcLen)
	requireWriteUint64Le(ctx, mod.Memory(), "n", sp+40, n)
	requireWriteByte(ctx, mod.Memory(), "ok", sp+48, byte(ok))
}

// copyBytesToJS implements js.copyBytesToJS.
//
// Results
//
//	* n is the count of bytes written.
//	* ok is false if the dst was not a uint8Array.
//
// See https://github.com/golang/go/blob/go1.19beta1/src/syscall/js/js.go#L583
//     https://github.com/golang/go/blob/go1.19beta1/misc/wasm/wasm_exec.js#L438-L448
func (j *jsWasm) copyBytesToJS(ctx context.Context, mod api.Module, dstRef, srcAddr, srcLen uint64) (n uint64, ok uint32) {
	src := requireRead(ctx, mod.Memory(), "src", uint32(srcAddr), uint32(srcLen)) // nolint
	v := j.loadValue(ctx, mod, uint32(dstRef))
	if dst, ok := v.(uint8Array); ok {
		return uint64(copy(dst, src)), 1
	}
	return 0, 0
}

// reflectGet implements JavaScript's Reflect.get API.
func (j *jsWasm) reflectGet(target interface{}, propertyKey string) interface{} { // nolint
	panic(fmt.Errorf("TODO: reflectGet(target=%v, propertyKey=%s)", target, propertyKey))
}

// reflectGet implements JavaScript's Reflect.get API for an index.
func (j *jsWasm) reflectGetIndex(target interface{}, i int) interface{} { // nolint
	panic(fmt.Errorf("TODO: reflectGetIndex(target=%v, t=%d)", target, i))
}

// reflectSet implements JavaScript's Reflect.set API.
func (j *jsWasm) reflectSet(target interface{}, propertyKey string, value interface{}) { // nolint
	panic(fmt.Errorf("TODO: reflectSet(target=%v, propertyKey=%s, value=%v)", target, propertyKey, value))
}

// reflectSetIndex implements JavaScript's Reflect.set API for an index.
func (j *jsWasm) reflectSetIndex(target interface{}, i int, value interface{}) { // nolint
	panic(fmt.Errorf("TODO: reflectSetIndex(target=%v, i=%d, value=%v)", target, i, value))
}

// reflectDeleteProperty implements JavaScript's Reflect.deleteProperty API
func (j *jsWasm) reflectDeleteProperty(target interface{}, propertyKey string) { // nolint
	panic(fmt.Errorf("TODO: reflectDeleteProperty(target=%v, propertyKey=%s)", target, propertyKey))
}

// reflectApply implements JavaScript's Reflect.apply API
func (j *jsWasm) reflectApply(target interface{}, thisArgument interface{}, argumentsList []interface{}) (interface{}, error) { // nolint
	panic(fmt.Errorf("TODO: reflectApply(target=%v, thisArgument=%v, argumentsList=%v)", target, thisArgument, argumentsList))
}

// reflectConstruct implements JavaScript's Reflect.construct API
func (j *jsWasm) reflectConstruct(target interface{}, argumentsList []interface{}) (interface{}, error) { // nolint
	panic(fmt.Errorf("TODO: reflectConstruct(target=%v, argumentsList=%v)", target, argumentsList))
}

// valueRef returns 8 bytes to represent either the value or a reference to it.
// Any side effects besides memory must be cleaned up on wasmExit.
//
// See https://github.com/golang/go/blob/go1.19beta1/misc/wasm/wasm_exec.js#L135-L183
func (j *jsWasm) valueRef(ctx context.Context, mod api.Module, v interface{}) uint64 { // nolint
	panic(fmt.Errorf("TODO: valueRef(%v)", v))
}

// loadValue reads up to 8 bytes at the memory offset `addr` to return the
// value written by storeValue.
//
// See https://github.com/golang/go/blob/go1.19beta1/misc/wasm/wasm_exec.js#L122-L133
func (j *jsWasm) loadValue(ctx context.Context, mod api.Module, addr uint32) interface{} { // nolint
	panic(fmt.Errorf("TODO: loadValue(%d)", addr))
}

// loadSliceOfValues returns a slice of `len` values at the memory offset
// `addr`
//
// See https://github.com/golang/go/blob/go1.19beta1/misc/wasm/wasm_exec.js#L191-L199
func (j *jsWasm) loadSliceOfValues(ctx context.Context, mod api.Module, sliceAddr, sliceLen uint32) []interface{} { // nolint
	result := make([]interface{}, sliceLen)
	for i := uint32(0); i < sliceLen; i++ { // nolint
		result = append(result, j.loadValue(ctx, mod, sliceAddr+i*8))
	}
	return result
}

// valueString returns the string form of JavaScript string, boolean and number types.
func (j *jsWasm) valueString(v interface{}) string { // nolint
	panic(fmt.Errorf("TODO: valueString(%v)", v))
}

// instanceOf returns true if the value is of the given type.
func (j *jsWasm) instanceOf(v, t interface{}) bool { // nolint
	panic(fmt.Errorf("TODO: instanceOf(v=%v, t=%v)", v, t))
}

// failIfClosed returns a sys.ExitError if wasmExit was called.
func (j *jsWasm) failIfClosed(mod api.Module) error {
	if closed := atomic.LoadUint64(j.closed); closed != 0 {
		return sys.NewExitError(mod.Name(), uint32(closed>>32)) // Unpack the high order bits as the exit code.
	}
	return nil
}

// getSysCtx returns the sys.Context from the module or panics.
func getSysCtx(mod api.Module) *internalsys.Context {
	if internal, ok := mod.(*wasm.CallContext); !ok {
		panic(fmt.Errorf("unsupported wasm.Module implementation: %v", mod))
	} else {
		return internal.Sys
	}
}

// requireRead is like api.Memory except that it panics if the offset and
// byteCount are out of range.
func requireRead(ctx context.Context, mem api.Memory, fieldName string, offset, byteCount uint32) []byte {
	buf, ok := mem.Read(ctx, offset, byteCount)
	if !ok {
		panic(fmt.Errorf("Memory.Read(ctx, %d, %d) out of range of memory size %d reading %s",
			offset, byteCount, mem.Size(ctx), fieldName))
	}
	return buf
}

// requireReadUint32Le is like api.Memory except that it panics if the offset
// is out of range.
func requireReadUint32Le(ctx context.Context, mem api.Memory, fieldName string, offset uint32) uint32 {
	result, ok := mem.ReadUint32Le(ctx, offset)
	if !ok {
		panic(fmt.Errorf("Memory.ReadUint64Le(ctx, %d) out of range of memory size %d reading %s",
			offset, mem.Size(ctx), fieldName))
	}
	return result
}

// requireReadUint64Le is like api.Memory except that it panics if the offset
// is out of range.
func requireReadUint64Le(ctx context.Context, mem api.Memory, fieldName string, offset uint32) uint64 {
	result, ok := mem.ReadUint64Le(ctx, offset)
	if !ok {
		panic(fmt.Errorf("Memory.ReadUint64Le(ctx, %d) out of range of memory size %d reading %s",
			offset, mem.Size(ctx), fieldName))
	}
	return result
}

// requireWriteByte is like api.Memory except that it panics if the offset
// is out of range.
func requireWriteByte(ctx context.Context, mem api.Memory, fieldName string, offset uint32, val byte) {
	if ok := mem.WriteByte(ctx, offset, val); !ok {
		panic(fmt.Errorf("Memory.WriteByte(ctx, %d, %d) out of range of memory size %d writing %s",
			offset, val, mem.Size(ctx), fieldName))
	}
}

// requireWriteUint32Le is like api.Memory except that it panics if the offset
// is out of range.
func requireWriteUint32Le(ctx context.Context, mem api.Memory, fieldName string, offset uint32, val uint32) {
	if ok := mem.WriteUint32Le(ctx, offset, val); !ok {
		panic(fmt.Errorf("Memory.WriteUint32Le(ctx, %d, %d) out of range of memory size %d writing %s",
			offset, val, mem.Size(ctx), fieldName))
	}
}

// requireWriteUint64Le is like api.Memory except that it panics if the offset
// is out of range.
func requireWriteUint64Le(ctx context.Context, mem api.Memory, fieldName string, offset uint32, val uint64) {
	if ok := mem.WriteUint64Le(ctx, offset, val); !ok {
		panic(fmt.Errorf("Memory.WriteUint64Le(ctx, %d, %d) out of range of memory size %d writing %s",
			offset, val, mem.Size(ctx), fieldName))
	}
}

// refreshSP refreshes the stack pointer, which is needed prior to storeValue
// when in an operation that can trigger a Go event handler.
//
// See https://github.com/golang/go/blob/go1.19beta1/misc/wasm/wasm_exec.js#L210-L213
func (j *jsWasm) refreshSP(ctx context.Context, mod api.Module) uint32 {
	ret, err := mod.ExportedFunction("getsp").Call(ctx) // refresh the stack pointer
	if err != nil {
		panic(fmt.Errorf("error refreshing stack pointer: %w", err))
	}
	return uint32(ret[0])
}

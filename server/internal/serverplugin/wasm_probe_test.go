package serverplugin

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

func TestWasmProbe(t *testing.T) {
	dir := t.TempDir()
	wasmPath := filepath.Join(dir, "probe.wasm")
	cmd := exec.Command("go", "build", "-o", wasmPath, "./testdata/probe")
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithCloseOnContextDone(true))
	defer r.Close(ctx)
	wasi_snapshot_preview1.MustInstantiate(ctx, r)
	b := r.NewHostModuleBuilder("env")
	b.NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		mem := mod.Memory()
		opPtr, opLen := uint32(stack[0]), uint32(stack[1])
		reqPtr, reqLen := uint32(stack[2]), uint32(stack[3])
		outPtr, outCap := uint32(stack[4]), uint32(stack[5])
		op, _ := mem.Read(opPtr, opLen)
		req, _ := mem.Read(reqPtr, reqLen)
		res := append(append([]byte{}, op...), ':')
		res = append(res, req...)
		if uint32(len(res)) > outCap || !mem.Write(outPtr, res) {
			stack[0] = ^uint64(0)
			return
		}
		stack[0] = uint64(int64(len(res)))
	}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).Export("mc_call")
	if _, err := b.Instantiate(ctx); err != nil {
		t.Fatal(err)
	}
	compiled, err := r.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	_, err = r.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithStdin(bytes.NewBufferString(`{"hello":"world"}`)).WithStdout(&stdout).WithStderr(&stderr))
	if err != nil {
		t.Fatalf("instantiate: %v, stderr=%s", err, stderr.String())
	}
	if got := stdout.String(); got != `echo:{"hello":"world"}` {
		t.Fatalf("got %q stderr=%s", got, stderr.String())
	}
}

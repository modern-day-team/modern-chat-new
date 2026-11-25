//go:build wasip1

package main

import (
	"fmt"
	"io"
	"os"
	"unsafe"
)

//go:wasmimport env mc_call
func mcCall(opPtr, opLen, reqPtr, reqLen, outPtr, outCap uint32) int32

func hostCall(op string, req []byte) ([]byte, error) {
	out := make([]byte, 4096)
	var opPtr uint32
	if len(op) > 0 {
		opPtr = uint32(uintptr(unsafe.Pointer(unsafe.StringData(op))))
	}
	var reqPtr uint32
	if len(req) > 0 {
		reqPtr = uint32(uintptr(unsafe.Pointer(&req[0])))
	}
	outPtr := uint32(uintptr(unsafe.Pointer(&out[0])))
	n := mcCall(opPtr, uint32(len(op)), reqPtr, uint32(len(req)), outPtr, uint32(len(out)))
	if n < 0 {
		return nil, fmt.Errorf("host call %s failed: %d", op, n)
	}
	return out[:n], nil
}

func main() {
	body, _ := io.ReadAll(os.Stdin)
	echo, err := hostCall("echo", body)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_, _ = os.Stdout.Write(echo)
}

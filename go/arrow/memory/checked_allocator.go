// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package memory

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"
)

type CheckedAllocator struct {
	mem Allocator
	sz  int64

	allocs sync.Map
}

func NewCheckedAllocator(mem Allocator) *CheckedAllocator {
	return &CheckedAllocator{mem: mem}
}

func (a *CheckedAllocator) CurrentAlloc() int { return int(atomic.LoadInt64(&a.sz)) }

func (a *CheckedAllocator) Allocate(size int) []byte {
	atomic.AddInt64(&a.sz, int64(size))
	out := a.mem.Allocate(size)
	if size == 0 {
		return out
	}

	ptr := uintptr(unsafe.Pointer(&out[0]))
	pcs := make([]uintptr, maxRetainedFrames)
	runtime.Callers(allocFrames, pcs)
	callersFrames := runtime.CallersFrames(pcs)
	if pc, _, l, ok := runtime.Caller(allocFrames); ok {
		a.allocs.Store(ptr, &dalloc{pc: pc, line: l, sz: size, callersFrames: callersFrames})
	}
	return out
}

func (a *CheckedAllocator) Reallocate(size int, b []byte) []byte {
	atomic.AddInt64(&a.sz, int64(size-len(b)))

	oldptr := uintptr(unsafe.Pointer(&b[0]))
	out := a.mem.Reallocate(size, b)
	if size == 0 {
		return out
	}

	newptr := uintptr(unsafe.Pointer(&out[0]))
	a.allocs.Delete(oldptr)
	pcs := make([]uintptr, maxRetainedFrames)
	runtime.Callers(reallocFrames, pcs)
	callersFrames := runtime.CallersFrames(pcs)
	if pc, _, l, ok := runtime.Caller(reallocFrames); ok {
		a.allocs.Store(newptr, &dalloc{pc: pc, line: l, sz: size, callersFrames: callersFrames})
	}

	return out
}

func (a *CheckedAllocator) Free(b []byte) {
	atomic.AddInt64(&a.sz, int64(len(b)*-1))
	defer a.mem.Free(b)

	if len(b) == 0 {
		return
	}

	ptr := uintptr(unsafe.Pointer(&b[0]))
	a.allocs.Delete(ptr)
}

// typically the allocations are happening in memory.Buffer, not by consumers calling
// allocate/reallocate directly. As a result, we want to skip the caller frames
// of the inner workings of Buffer in order to find the caller that actually triggered
// the allocation via a call to Resize/Reserve/etc.
const (
	defAllocFrames       = 4
	defReallocFrames     = 3
	defMaxRetainedFrames = 0
)

// Use the environment variables ARROW_CHECKED_ALLOC_FRAMES and ARROW_CHECKED_REALLOC_FRAMES
// to control how many frames it skips when storing the caller for allocations/reallocs
// when using this to find memory leaks. Use ARROW_CHECKED_MAX_RETAINED_FRAMES to control how
// many frames are retained for printing the stack trace of a leak.
var allocFrames, reallocFrames, maxRetainedFrames int = defAllocFrames, defReallocFrames, defMaxRetainedFrames

func init() {
	if val, ok := os.LookupEnv("ARROW_CHECKED_ALLOC_FRAMES"); ok {
		if f, err := strconv.Atoi(val); err == nil {
			allocFrames = f
		}
	}

	if val, ok := os.LookupEnv("ARROW_CHECKED_REALLOC_FRAMES"); ok {
		if f, err := strconv.Atoi(val); err == nil {
			reallocFrames = f
		}
	}

	if val, ok := os.LookupEnv("ARROW_CHECKED_MAX_RETAINED_FRAMES"); ok {
		if f, err := strconv.Atoi(val); err == nil {
			maxRetainedFrames = f
		}
	}
}

type dalloc struct {
	pc            uintptr
	line          int
	sz            int
	callersFrames *runtime.Frames
}

type TestingT interface {
	Errorf(format string, args ...interface{})
	Helper()
}

func (a *CheckedAllocator) AssertSize(t TestingT, sz int) {
	a.allocs.Range(func(_, value interface{}) bool {
		info := value.(*dalloc)
		f := runtime.FuncForPC(info.pc)
		frames := info.callersFrames
		var callersMsg strings.Builder
		for {
			frame, more := frames.Next()
			if frame.Line == 0 {
				break
			}
			callersMsg.WriteString("\t")
			callersMsg.WriteString(frame.Function)
			callersMsg.WriteString(fmt.Sprintf(" line %d", frame.Line))
			callersMsg.WriteString("\n")
			if !more {
				break
			}
		}
		t.Errorf("LEAK of %d bytes FROM %s line %d\n%v", info.sz, f.Name(), info.line, callersMsg.String())
		return true
	})

	if int(atomic.LoadInt64(&a.sz)) != sz {
		t.Helper()
		t.Errorf("invalid memory size exp=%d, got=%d", sz, a.sz)
	}
}

type CheckedAllocatorScope struct {
	alloc *CheckedAllocator
	sz    int
}

func NewCheckedAllocatorScope(alloc *CheckedAllocator) *CheckedAllocatorScope {
	sz := atomic.LoadInt64(&alloc.sz)
	return &CheckedAllocatorScope{alloc: alloc, sz: int(sz)}
}

func (c *CheckedAllocatorScope) CheckSize(t TestingT) {
	sz := int(atomic.LoadInt64(&c.alloc.sz))
	if c.sz != sz {
		t.Helper()
		t.Errorf("invalid memory size exp=%d, got=%d", c.sz, sz)
	}
}

var (
	_ Allocator = (*CheckedAllocator)(nil)
)

package objc

import (
	"reflect"
	"runtime"
	"testing"
)

func TestCCRouterEmptyNSArrayAndNarrowReturn(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	WithAutoreleasePool(func() {
		array := Call[Object](GetClass("NSArray"), Sel("array"))
		v := ToGoSlice(array.Ptr(), reflect.TypeOf([]Object{}))
		if v.Len() != 0 {
			t.Fatal("empty NSArray converted incorrectly")
		}
		number := Call[Object](GetClass("NSNumber"), Sel("numberWithBool:"), true)
		for i := 0; i < 10000; i++ {
			if !Call[bool](number, Sel("boolValue")) {
				t.Fatal("narrow return corrupted")
			}
		}
	})
	// Exercise the CIF finalizers after many small native return values.
	runtime.GC()
	runtime.GC()
}

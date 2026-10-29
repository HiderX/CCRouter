# CCRouter compatibility fixes

Base: progrium/darwinkit `61b9e31a12fa033c8f3441e596e5237d396ed960`.
Imported through pmoust/darwinkit `0a368ccd4ca9e3a4961099180d557c0d297221e7`
([upstream PR #289](https://github.com/progrium/darwinkit/pull/289)). Original licenses are retained.

This copy keeps the application's Go build reproducible while upstream fixes are pending.
Application code remains in `internal/`; do not edit generated framework bindings for product behavior.

Changes relative to the base:

1. `objc/ffi/ffi.go`: PR #289 retains CIF return/argument types for the full CIF lifetime.
2. `objc/call.go`: reserve at least eight bytes for narrow libffi return values. libffi's
   integral return buffer must be large enough for `ffi_arg`, including when the Go result
   is a bool or byte. Without padding, a native call can overwrite adjacent Go allocations.
3. `objc/type_convertion.m`: zero-initialize returned array/dictionary descriptors. The old
   empty-array path returned uninitialized length/data, causing impossible allocation sizes.

Recheck these fixes and the native smoke test before replacing this copy with a newer upstream release.

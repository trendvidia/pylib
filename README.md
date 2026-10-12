# pylib

CPython inside a Go program. The interpreter is linked statically through
cgo, and its standard library is embedded and unpacked once into a cache
directory. Nothing is needed at run time beyond the system's C library.

```go
if err := pylib.Start(cacheDir, extraImportPaths...); err != nil { ... }
out, err := pylib.Run(`OUT = IN.upper()`, "hello") // "HELLO"
```

A process has one interpreter. `Run` may be called from any goroutine; calls
take turns under the GIL.

**Targets:** darwin/arm64, darwin/amd64, linux/arm64, linux/amd64 (glibc).
**CPython:** 3.14.8, from [python-build-standalone](https://github.com/astral-sh/python-build-standalone)
release 20261003.
**Bundled libraries:** OpenSSL 3.5.9, SQLite 3.53.1, zlib 1.3.2, zstd 1.5.7, with the rest of that release's set. Security fixes to any of them arrive by rebuilding from a newer release (see `SECURITY.md`). On macOS, zlib, ncurses and libedit are the system's.

## How it is built

`scripts/build.py` regenerates `deps/` and `link_*.go`:
- It downloads python-build-standalone's `pgo+lto-full` archive for each target.
- Those objects are LLVM bitcode, so it compiles each one to a native object for that target. This needs an LLVM at least as new as the one that wrote them: LLVM 22 for 20261003, Homebrew's `llvm` here.
- It takes the object list from the archive's `PYTHON.json`, not from `libpython.a`, whose member names repeat.
- It includes every extension module CPython's builtin table names (100 on Linux, 101 on macOS). The shared-library-only ones are left out: `_tkinter` and the test modules.
- It strips debug info.
- It packs the standard library, minus tests, tkinter, idlelib and ensurepip, as `deps/<platform>/stdlib.tar.gz`.

To link, use the system toolchain or a cross compiler: `clang -arch <arch>` for darwin, and gcc or `zig cc -target <arch>-linux-gnu` for linux, with `CGO_LDFLAGS` cleared.

## Licenses

pylib is under the MIT license (`LICENSE`).

CPython is under the PSF license. The libraries linked with it (OpenSSL, SQLite, libffi, zlib, xz, bzip2, zstd, expat, mpdecimal, ncurses, libedit, libuuid) are under their own licenses. These are in `deps/<platform>/licenses/`, along with the notices for the code CPython incorporates (mimalloc, HACL*, and the rest of its `Doc/license.rst`). Most come from python-build-standalone. `scripts/build.py` adds the ones it lacks from `scripts/licenses/`.

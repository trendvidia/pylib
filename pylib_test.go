// SPDX-License-Identifier: MIT
// Copyright (c) 2026 TrendVidia, LLC.

package pylib

import (
	"os"
	"strings"
	"sync"
	"testing"
)

// cache is the one cache directory of the test process: the interpreter
// starts once, and reads its standard library from there for the rest of
// the run.
var cache string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "pylib-test-")
	if err != nil {
		panic(err)
	}
	cache = dir
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// TestRun starts the interpreter and imports the C extension modules a
// static build is most likely to lose — zlib, the JSON and SQLite
// accelerators, hashlib's OpenSSL, ctypes — and reads a UTF-8 literal.
func TestRun(t *testing.T) {
	if err := Start(cache); err != nil {
		t.Fatal(err)
	}
	out, err := Run(`
import sys, zlib, json, _json, sqlite3, hashlib, ctypes, ssl, decimal, lzma, bz2
OUT = "%s %s %s %d" % (sys.version.split()[0], sys.flags.utf8_mode, "é" + IN, len(sys.builtin_module_names))
`, "x")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, Version+" 1 éx ") {
		t.Errorf("Run = %q, want %s, UTF-8 mode and the input", out, Version)
	}
}

// TestRunError: an exception is an error carrying its traceback, and the
// interpreter stays usable after it.
func TestRunError(t *testing.T) {
	if err := Start(cache); err != nil {
		t.Fatal(err)
	}
	if _, err := Run("raise ValueError('nope')", ""); err == nil || !strings.Contains(err.Error(), "ValueError: nope") {
		t.Errorf("Run = %v, want the ValueError's traceback", err)
	}
	if out, err := Run("OUT = 'ok'", ""); err != nil || out != "ok" {
		t.Errorf("Run after an error = %q, %v", out, err)
	}
}

// TestRunConcurrent: goroutines take turns under the GIL.
func TestRunConcurrent(t *testing.T) {
	if err := Start(cache); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			want := strings.Repeat("x", i)
			if out, err := Run("OUT = IN", want); err != nil || out != want {
				t.Errorf("Run = %q, %v, want %q", out, err, want)
			}
		}()
	}
	wg.Wait()
}

// TestUnpack: a second unpack of the same content reuses the first
// directory, and different content gets its own.
func TestUnpack(t *testing.T) {
	dir := t.TempDir()
	a, err := Unpack(dir, "std", stdlib)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a + "/os.py"); err != nil {
		t.Errorf("os.py not unpacked: %v", err)
	}
	b, err := Unpack(dir, "std", stdlib)
	if err != nil || b != a {
		t.Errorf("second Unpack = %q, %v, want %q", b, err, a)
	}
	if _, err := Unpack(dir, "std", []byte("not gzip")); err == nil {
		t.Error("Unpack of a non-gzip payload succeeded")
	}
}

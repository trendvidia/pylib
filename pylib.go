// Package pylib runs CPython inside a Go program: the interpreter linked
// statically through cgo, its standard library embedded and unpacked once
// into a cache directory.
//
// The interpreter is python-build-standalone's build of CPython, compiled
// to native objects for darwin/arm64, darwin/amd64, linux/arm64 and
// linux/amd64 by scripts/build.py. A process has one interpreter: Start
// initializes it, and Run executes code in it from any goroutine, one call
// at a time under the GIL.
package pylib

/*
#include <Python.h>
#include <stdlib.h>

static PyObject* runner;

// start initializes the interpreter, isolated from the environment and
// the user's site directory, in UTF-8 mode, with the given import path,
// and leaves the GIL released.
static char* start(wchar_t** paths, int n) {
	PyPreConfig pre;
	PyPreConfig_InitIsolatedConfig(&pre);
	pre.utf8_mode = 1;
	PyStatus s = Py_PreInitialize(&pre);
	if (PyStatus_Exception(s)) return strdup(s.err_msg ? s.err_msg : "pre-initialization failed");
	PyConfig c;
	PyConfig_InitIsolatedConfig(&c);
	c.write_bytecode = 0;
	c.install_signal_handlers = 0;
	c.module_search_paths_set = 1;
	for (int i = 0; i < n; i++) PyWideStringList_Append(&c.module_search_paths, paths[i]);
	s = Py_InitializeFromConfig(&c);
	PyConfig_Clear(&c);
	if (PyStatus_Exception(s)) return strdup(s.err_msg ? s.err_msg : "initialization failed");
	PyObject* g = PyDict_New();
	PyDict_SetItemString(g, "__builtins__", PyEval_GetBuiltins());
	PyObject* r = PyRun_String(
		"def run(code, inp):\n"
		"    g = {'__name__': '__main__', 'IN': inp}\n"
		"    try:\n"
		"        exec(compile(code, '<run>', 'exec'), g)\n"
		"        return True, str(g.get('OUT', ''))\n"
		"    except BaseException:\n"
		"        import traceback\n"
		"        return False, traceback.format_exc()\n",
		Py_file_input, g, g);
	if (!r) { PyErr_Print(); return strdup("defining the runner failed"); }
	Py_DECREF(r);
	runner = PyDict_GetItemString(g, "run");
	Py_INCREF(runner);
	PyEval_SaveThread();
	return NULL;
}

// run calls the runner under the GIL. It returns the output, or the
// traceback with *failed set.
static char* run(const char* code, const char* in, int* failed) {
	PyGILState_STATE st = PyGILState_Ensure();
	char* out = NULL;
	PyObject* r = PyObject_CallFunction(runner, "ss", code, in);
	if (!r) {
		PyErr_Print();
		*failed = 1;
		out = strdup("the runner raised");
	} else {
		*failed = !PyObject_IsTrue(PyTuple_GetItem(r, 0));
		const char* u = PyUnicode_AsUTF8(PyTuple_GetItem(r, 1));
		out = strdup(u ? u : "");
		Py_DECREF(r);
	}
	PyGILState_Release(st);
	return out;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"
)

// Version is the CPython version linked.
const Version = "3.14.8"

var (
	started  bool
	startErr error
	startMu  sync.Mutex
)

// Start initializes the interpreter once per process: the standard
// library unpacked under cacheDir, then paths, as its import path. A
// second call returns the first one's result.
func Start(cacheDir string, paths ...string) error {
	startMu.Lock()
	defer startMu.Unlock()
	if started {
		return startErr
	}
	started = true
	lib, err := unpack(cacheDir)
	if err != nil {
		startErr = err
		return err
	}
	all := append([]string{lib}, paths...)
	ws := make([]*C.wchar_t, len(all))
	for i, p := range all {
		cs := C.CString(p)
		ws[i] = C.Py_DecodeLocale(cs, nil)
		C.free(unsafe.Pointer(cs))
		if ws[i] == nil {
			startErr = fmt.Errorf("pylib: path %q does not decode", p)
			return startErr
		}
	}
	if msg := C.start((**C.wchar_t)(unsafe.Pointer(&ws[0])), C.int(len(ws))); msg != nil {
		startErr = errors.New("pylib: " + C.GoString(msg))
		C.free(unsafe.Pointer(msg))
	}
	return startErr
}

// Run executes code as a module with the global IN set to in, and returns
// the string its global OUT holds afterwards. An exception is an error
// carrying the traceback.
func Run(code, in string) (string, error) {
	startMu.Lock()
	ok := started && startErr == nil
	startMu.Unlock()
	if !ok {
		return "", errors.New("pylib: Run before a successful Start")
	}
	cc, ci := C.CString(code), C.CString(in)
	defer C.free(unsafe.Pointer(cc))
	defer C.free(unsafe.Pointer(ci))
	var failed C.int
	out := C.run(cc, ci, &failed)
	defer C.free(unsafe.Pointer(out))
	if failed != 0 {
		return "", errors.New("pylib: " + C.GoString(out))
	}
	return C.GoString(out), nil
}

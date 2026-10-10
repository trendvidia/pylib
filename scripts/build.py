#!/usr/bin/env python3
"""Rebuild the static CPython this module links, for every target.

python-build-standalone's full archives ship CPython's object files and a
PYTHON.json saying how to link them. The objects are LLVM bitcode (its
pgo+lto builds), which a linker older than the LLVM that wrote them cannot
read, so each is compiled here to a native object, for the archive's own
target, with an LLVM at least as new. The objects are taken from
PYTHON.json, not from libpython's archive, whose member names repeat.

Every extension module CPython's own inittab names is included; the ones
it builds as shared libraries (_tkinter and the test modules) are not.

Usage: scripts/build.py            all targets
       scripts/build.py TRIPLE...  some
Needs: gh, zstd, and LLVM >= the bitcode's (LLVM env var, default
Homebrew's /opt/homebrew/opt/llvm/bin).
"""
import concurrent.futures as cf
import gzip
import json
import os
import shutil
import subprocess
import sys
import tarfile

RELEASE = "20261003"
VERSION = "3.14.8"
TARGETS = {
    "aarch64-apple-darwin": "darwin_arm64",
    "x86_64-apple-darwin": "darwin_amd64",
    "aarch64-unknown-linux-gnu": "linux_arm64",
    "x86_64-unknown-linux-gnu": "linux_amd64",
}
# Left out of the payload: tests, GUI and IDE modules, what pip installs,
# and the build's own config. Compiled bytecode is rebuilt on demand.
STDLIB_SKIP = {"test", "idlelib", "tkinter", "turtledemo", "ensurepip", "site-packages", "lib-dynload", "__pycache__"}

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CACHE = os.environ.get("CACHE", os.path.expanduser("~/.cache/pylib"))
LLVM = os.environ.get("LLVM", "/opt/homebrew/opt/llvm/bin")


def run(*args, **kw):
    return subprocess.run(args, check=True, **kw)


def fetch(triple):
    d = os.path.join(CACHE, f"{VERSION}-{RELEASE}-{triple}")
    if not os.path.exists(os.path.join(d, "python", "PYTHON.json")):
        os.makedirs(d, exist_ok=True)
        name = f"cpython-{VERSION}+{RELEASE}-{triple}-pgo+lto-full.tar.zst"
        run("gh", "release", "download", RELEASE, "-R", "astral-sh/python-build-standalone", "-p", name, "-D", d, "--clobber")
        run("sh", "-c", f'cd "{d}" && zstd -dc "{name}" | tar -x && rm "{name}"')
    return os.path.join(d, "python")


def native(src, dst, triple):
    """Compiles a bitcode object to a native one, or copies a native one."""
    kind = subprocess.run(["file", "-b", src], capture_output=True, text=True).stdout
    if "bitcode" not in kind:
        shutil.copy(src, dst)
        return
    target = triple.replace("apple-darwin", "apple-macosx")
    run(f"{LLVM}/clang", "-target", target, "-c", "-O2", "-fPIC", "-g0", "-Wno-override-module", "-x", "ir", src, "-o", dst,
        capture_output=True)


def build(triple):
    plat = TARGETS[triple]
    py = fetch(triple)
    info = json.load(open(os.path.join(py, "PYTHON.json")))
    b = info["build_info"]
    inittab = open(os.path.join(py, "build", "Modules", "config.c")).read()
    objs, links = list(b["core"]["objs"]) + [b["inittab_object"]], []
    for name, variants in sorted(b["extensions"].items()):
        e = variants[0]
        if e.get("in_core") or e["init_fn"] not in inittab:
            continue
        objs += e.get("objs", [])
        links += [l for l in e.get("links", []) if l not in links]
    links += [l for l in b["core"]["links"] if l not in links]

    out = os.path.join(ROOT, "deps", plat)
    shutil.rmtree(out, ignore_errors=True)
    os.makedirs(out)
    work = os.path.join(CACHE, "objs-" + triple)
    shutil.rmtree(work, ignore_errors=True)
    os.makedirs(work)
    objs = sorted(set(objs))
    with cf.ThreadPoolExecutor(os.cpu_count()) as ex:
        list(ex.map(lambda o: native(os.path.join(py, o), os.path.join(work, o.replace("/", "__")), triple), objs))
    run(f"{LLVM}/llvm-ar", "rcs", os.path.join(out, "libpython.a"), *[os.path.join(work, o.replace("/", "__")) for o in objs])

    flags = ["${SRCDIR}/deps/%s/libpython.a" % plat]
    for l in links:
        if l.get("framework"):
            flags += ["-framework", l["name"]]
        elif l.get("path_static"):
            base = os.path.basename(l["path_static"])
            shutil.copy(os.path.join(py, l["path_static"]), os.path.join(out, base))
            flags.append("${SRCDIR}/deps/%s/%s" % (plat, base))
        else:
            flags.append("-l" + l["name"])
    if plat.startswith("linux_"):
        # GNU ld reads each archive once, left to right, and PYTHON.json's
        # order puts libncursesw before libedit and libcrypto before libssl,
        # which need them: a group is rescanned until nothing more
        # resolves. lld and Apple's ld search every archive anyway.
        flags = ["-Wl,--start-group"] + flags + ["-Wl,--end-group"]
    for a in os.listdir(out):
        run(f"{LLVM}/llvm-strip", "--strip-debug", os.path.join(out, a))

    shutil.copytree(os.path.join(py, info["python_paths"]["include"]), os.path.join(out, "include"))
    stdlib = os.path.join(py, info["python_paths"]["stdlib"])
    # gzip's header carries a time and a name unless told not to; both are
    # zeroed so a rebuild of the same release is byte for byte the same.
    gz = gzip.GzipFile(os.path.join(out, "stdlib.tar.gz"), "wb", compresslevel=9, mtime=0)
    gz.name = ""
    with tarfile.open(fileobj=gz, mode="w") as t:
        for d, dirs, files in os.walk(stdlib):
            dirs[:] = sorted(x for x in dirs if x not in STDLIB_SKIP and not x.startswith("config-"))
            for f in sorted(files):
                p = os.path.join(d, f)
                ti = t.gettarinfo(p, os.path.relpath(p, stdlib))
                ti.mtime, ti.uid, ti.gid, ti.uname, ti.gname = 0, 0, 0, "", ""
                with open(p, "rb") as fh:
                    t.addfile(ti, fh)
    gz.close()
    shutil.copytree(os.path.join(py, "licenses"), os.path.join(out, "licenses"))

    goos, goarch = plat.split("_")
    with open(os.path.join(ROOT, f"link_{plat}.go"), "w") as f:
        f.write(f"""// Code generated by scripts/build.py from python-build-standalone {RELEASE} ({triple}); DO NOT EDIT.

package pylib

/*
#cgo CFLAGS: -I${{SRCDIR}}/deps/{plat}/include
#cgo LDFLAGS: {' '.join(flags)}
*/
import "C"

import _ "embed"

//go:embed deps/{plat}/stdlib.tar.gz
var stdlib []byte
""")
    print(triple, len(objs), "objects")


if __name__ == "__main__":
    for t in sys.argv[1:] or list(TARGETS):
        build(t)

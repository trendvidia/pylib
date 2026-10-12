# Security

## Reporting a vulnerability

Report it privately through GitHub: **Security → Report a vulnerability** on this repository. Please don't open a public issue for it.

## What pylib does and doesn't protect

`Run` executes the Python code it is given with the full rights of the Go process. It is not a sandbox. Only pass it code you would run yourself.

## The bundled libraries

pylib links its own copies of CPython and the libraries it needs (see the README for versions). A vulnerability in one of them is fixed here by rebuilding from a newer python-build-standalone release with `scripts/build.py`. If one affects pylib, report it as above, or open an issue asking for the rebuild if it is already public.

package pylib

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// unpack writes the embedded standard library under cacheDir, in a
// directory named by its content, once: a complete unpack is renamed into
// place, so a process never sees half of one, and two racing processes
// both end with the same tree.
func unpack(cacheDir string) (string, error) {
	sum := sha256.Sum256(stdlib)
	dir := filepath.Join(cacheDir, "cpython-"+Version+"-"+hex.EncodeToString(sum[:6]))
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("pylib: %w", err)
	}
	tmp, err := os.MkdirTemp(cacheDir, ".cpython-unpack-")
	if err != nil {
		return "", fmt.Errorf("pylib: %w", err)
	}
	defer os.RemoveAll(tmp)
	if err := untar(stdlib, tmp); err != nil {
		return "", fmt.Errorf("pylib: unpacking the standard library: %w", err)
	}
	if err := os.Rename(tmp, dir); err != nil {
		if _, statErr := os.Stat(dir); statErr == nil {
			return dir, nil // another process won
		}
		return "", fmt.Errorf("pylib: %w", err)
	}
	return dir, nil
}

func untar(gz []byte, dst string) error {
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return err
	}
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		p := filepath.Join(dst, filepath.FromSlash(h.Name))
		if !strings.HasPrefix(p, filepath.Clean(dst)+string(os.PathSeparator)) {
			return fmt.Errorf("entry %q leaves the tree", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		}
	}
}

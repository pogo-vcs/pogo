//go:build cgo
// +build cgo

package utils

import (
	"io"

	"github.com/DataDog/zstd"
)

func Compress(r io.Reader) io.Reader {
	pr, pw := io.Pipe()

	go func() {
		// defer r.Close()

		zw := zstd.NewWriterLevel(pw, zstd.DefaultCompression)

		_, copyErr := io.Copy(zw, r)

		closeErr := zw.Close()

		if copyErr != nil {
			pw.CloseWithError(copyErr)
		} else if closeErr != nil {
			pw.CloseWithError(closeErr)
		} else {
			pw.Close()
		}
	}()

	return pr
}

func Decompress(r io.Reader) io.Reader {
	pr, pw := io.Pipe()

	go func() {
		// defer r.Close()

		zr := zstd.NewReader(r)
		defer zr.Close()

		_, copyErr := io.Copy(pw, zr)

		if copyErr != nil {
			pw.CloseWithError(copyErr)
		} else {
			pw.Close()
		}
	}()

	return pr
}

//go:build !cgo
// +build !cgo

package compressions

import (
	"io"

	"github.com/klauspost/compress/zstd"
)

func Compress(r io.Reader) io.Reader {
	pr, pw := io.Pipe()

	go func() {
		// defer r.Close()

		zw, err := zstd.NewWriter(pw, zstd.WithEncoderLevel(zstd.SpeedBestCompression))
		if err != nil {
			pw.CloseWithError(err)
			return
		}

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

		zr, err := zstd.NewReader(r)
		if err != nil {
			pw.CloseWithError(err)
			return
		}
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

//go:build cgo

package compressions

import (
	"io"
	"sync"

	"github.com/DataDog/zstd"
)

func Compress(r io.ReadCloser) io.ReadCloser {
	pr, pw := io.Pipe()

	go func() {
		zw := zstd.NewWriterLevel(pw, zstd.DefaultCompression)

		_, copyErr := io.Copy(zw, r)

		closeErr := zw.Close()

		if copyErr != nil {
			pw.CloseWithError(copyErr)
		} else if closeErr != nil {
			pw.CloseWithError(closeErr)
		} else {
			_ = pw.Close()
		}
	}()

	return &closeWrapper{pr, r, sync.Mutex{}}
}

func Decompress(r io.ReadCloser) io.ReadCloser {
	pr, pw := io.Pipe()

	go func() {
		zr := zstd.NewReader(r)
		defer zr.Close()

		_, copyErr := io.Copy(pw, zr)

		if copyErr != nil {
			pw.CloseWithError(copyErr)
		} else {
			_ = pw.Close()
		}
	}()

	return &closeWrapper{pr, r, sync.Mutex{}}
}

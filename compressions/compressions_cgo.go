//go:build cgo

package compressions

import (
	"bytes"
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

func CompressBytes(data []byte) ([]byte, error) {
	return zstd.CompressLevel(nil, data, zstd.DefaultCompression)
}

func DecompressBytes(data []byte) ([]byte, error) {
	buf := bytes.NewBuffer(data)
	zr := zstd.NewReader(buf)
	defer zr.Close()
	return io.ReadAll(zr)
}

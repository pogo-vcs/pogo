//go:build !cgo

package compressions

import (
	"io"
	"sync"

	"github.com/klauspost/compress/zstd"
)

func Compress(r io.ReadCloser) io.ReadCloser {
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

	return &closeWrapper{pr, r, sync.Mutex{}}
}

func Decompress(r io.ReadCloser) io.ReadCloser {
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

	return &closeWrapper{pr, r, sync.Mutex{}}
}

var encoder, _ = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBestCompression))

func CompressBytes(data []byte) ([]byte, error) {
	return encoder.EncodeAll(data, nil), nil
}

func DecompressBytes(data []byte) ([]byte, error) {
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return nil, err
	}
	defer decoder.Close()
	return decoder.DecodeAll(data, nil)
}
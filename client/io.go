package client

import (
	"errors"

	"github.com/pogo-vcs/pogo/protos"
	"google.golang.org/grpc"
)

type PushFull_StreamWriter struct {
	stream grpc.ClientStreamingClient[protos.PushFullRequest, protos.PushFullResponse]
}

func (w *PushFull_StreamWriter) Write(p []byte) (n int, err error) {
	err = w.stream.Send(&protos.PushFullRequest{
		Payload: &protos.PushFullRequest_FileContent{
			FileContent: p,
		},
	})
	if err != nil {
		return 0, errors.Join(errors.New("write bytes to grpc stream"), err)
	}
	return len(p), nil
}
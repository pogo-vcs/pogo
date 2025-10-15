package server

import (
	"errors"
	"io"

	"github.com/pogo-vcs/pogo/protos"
	"google.golang.org/grpc"
)

type PushFull_StreamReader struct {
	stream grpc.ClientStreamingServer[protos.PushFullRequest, protos.PushFullResponse]
}

func (r PushFull_StreamReader) Read(p []byte) (int, error) {
	req, err := r.stream.Recv()
	if err != nil {
		return 0, err
	}
	switch v := req.Payload.(type) {
	case *protos.PushFullRequest_FileContent:
		return copy(p, v.FileContent), nil
	case *protos.PushFullRequest_Eof:
		return 0, io.EOF
	default:
		return 0, errors.New("invalid request payload")
	}
}

type Edit_StreamWriter struct {
	stream grpc.ServerStreamingServer[protos.EditResponse]
}

func (w Edit_StreamWriter) Write(p []byte) (int, error) {
	if err := w.stream.Send(&protos.EditResponse{
		Payload: &protos.EditResponse_FileContent{
			FileContent: p,
		},
	}); err != nil {
		return 0, err
	}
	return len(p), nil
}
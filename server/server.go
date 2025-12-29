package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/pogo-vcs/pogo/protos"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
)

type httpServer interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

type Server struct {
	protos.UnimplementedPogoServer
	httpMux    *http.ServeMux
	httpGoMux  httpServer
	grpcServer *grpc.Server
	server     *http.Server
}

func NewServer() *Server {
	s := &Server{
		httpMux:    http.NewServeMux(),
		httpGoMux:  newGoProxy(),
		grpcServer: grpc.NewServer(),
	}
	protos.RegisterPogoServer(s.grpcServer, s)
	RegisterWebUI(s)
	return s
}

func (a *Server) HandleFunc(pattern string, handler http.HandlerFunc) {
	a.httpMux.HandleFunc(pattern, handler)
}

func (a *Server) Handle(pattern string, handler http.Handler) {
	a.httpMux.Handle(pattern, handler)
}

func (a *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
		if r.ProtoMajor == 2 {
			a.grpcServer.ServeHTTP(w, r)
		} else {
			// gRPC requires HTTP/2 but received HTTP/1.1.
			// This happens when a reverse proxy (like Cloudflare) downgrades the connection.
			// Return a proper gRPC error instead of falling through to 404.
			w.Header().Set("Content-Type", "application/grpc")
			w.Header().Set("Grpc-Status", "14") // UNAVAILABLE
			w.Header().Set("Grpc-Message", "gRPC requires HTTP/2. Your reverse proxy is using HTTP/1.1. Enable 'gRPC' and 'HTTP/2 to Origin' in Cloudflare, or check your proxy configuration.")
			w.WriteHeader(http.StatusOK)
		}
	} else {
		if isGoProxyRequest(r) {
			a.httpGoMux.ServeHTTP(w, r)
			return
		}
		a.httpMux.ServeHTTP(w, r)
	}
}

func (a *Server) Start(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	fmt.Println("Listening on", ln.(*net.TCPListener).Addr())
	h2cHandler := h2c.NewHandler(a, &http2.Server{})
	a.server = &http.Server{
		Addr:    addr,
		Handler: h2cHandler,
	}
	go func() {
		if err := a.server.Serve(ln); err != nil {
			if err == http.ErrServerClosed {
				fmt.Println("Server closed")
			} else {
				fmt.Fprintln(os.Stderr, err)
			}
		}
	}()
	return nil
}

func (a *Server) Stop(ctx context.Context) error {
	if a.server != nil {
		if err := a.server.Shutdown(ctx); err != nil {
			return err
		}
		a.server = nil
	}
	return nil
}

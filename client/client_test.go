package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"
)

// createTestCertificate creates a self-signed certificate for testing
func createTestCertificate() (tls.Certificate, error) {
	// Generate a private key
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test Org"},
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)},
		DNSNames:    []string{"localhost"},
	}

	// Create the certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}

	// Create TLS certificate
	cert := tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  priv,
	}

	return cert, nil
}

func TestDetectTLSSupport(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("non_existent_server", func(t *testing.T) {
		// Test with a non-existent server - should return error
		supportsTLS, err := detectTLSSupport(ctx, "localhost:65432")
		if err == nil {
			t.Error("Expected detectTLSSupport to return error for non-existent server")
		}
		if supportsTLS {
			t.Error("Expected detectTLSSupport to return false for non-existent server")
		}
	})

	t.Run("http_only_server", func(t *testing.T) {
		// Start a plain HTTP server
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("Failed to create listener: %v", err)
		}
		defer listener.Close()

		server := &http.Server{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
		}

		go func() {
			server.Serve(listener)
		}()
		defer server.Shutdown(ctx)

		// Test detection - should return false (HTTP only, no TLS)
		addr := listener.Addr().String()
		supportsTLS, err := detectTLSSupport(ctx, addr)
		if err != nil {
			t.Errorf("Expected no error for HTTP server, got: %v", err)
		}
		if supportsTLS {
			t.Error("Expected detectTLSSupport to return false for HTTP-only server")
		}
	})

	t.Run("https_server", func(t *testing.T) {
		// Create test certificate
		cert, err := createTestCertificate()
		if err != nil {
			t.Fatalf("Failed to create test certificate: %v", err)
		}

		// Start HTTPS server
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("Failed to create listener: %v", err)
		}
		defer listener.Close()

		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{cert},
		}

		server := &http.Server{
			TLSConfig: tlsConfig,
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
		}

		go func() {
			server.ServeTLS(listener, "", "")
		}()
		defer server.Shutdown(ctx)

		// Give the server time to start
		time.Sleep(100 * time.Millisecond)

		// Test detection - should return true (HTTPS/TLS supported)
		addr := listener.Addr().String()
		supportsTLS, err := detectTLSSupport(ctx, addr)
		if err != nil {
			t.Errorf("Expected no error for HTTPS server, got: %v", err)
		}
		if !supportsTLS {
			t.Error("Expected detectTLSSupport to return true for HTTPS server")
		}
	})

	t.Run("external_https_server", func(t *testing.T) {
		// Test with external HTTPS server (network dependent)
		// Use a shorter timeout for external connections
		shortCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		supportsTLS, err := detectTLSSupport(shortCtx, "google.com:443")
		if err != nil {
			t.Skipf("Skipping external HTTPS test due to network error: %v", err)
		}
		if !supportsTLS {
			t.Errorf("Expected detectTLSSupport to return true for google.com:443, but got false")
		}
	})
}

func TestCreateGRPCClientWithTLSDetection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	t.Run("non_existent_server", func(t *testing.T) {
		var verboseOutput bytes.Buffer

		// Test with non-existent server - should return error
		client, err := createGRPCClientWithTLSDetection(ctx, "localhost:65432", &verboseOutput)
		if err == nil {
			t.Error("Expected createGRPCClientWithTLSDetection to fail for non-existent server")
		}
		if client != nil {
			client.Close()
			t.Error("Expected client to be nil when error occurs")
		}
	})

	t.Run("http_server", func(t *testing.T) {
		// Start a plain HTTP server
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("Failed to create listener: %v", err)
		}
		defer listener.Close()

		server := &http.Server{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
		}

		go func() {
			server.Serve(listener)
		}()
		defer server.Shutdown(ctx)

		var verboseOutput bytes.Buffer
		addr := listener.Addr().String()

		// Should succeed with HTTP fallback
		client, err := createGRPCClientWithTLSDetection(ctx, addr, &verboseOutput)
		if err != nil {
			t.Errorf("Expected success for HTTP server, got error: %v", err)
		}
		if client != nil {
			client.Close()
		}

		// Check verbose output mentions HTTP connection
		output := verboseOutput.String()
		if len(output) == 0 {
			t.Error("Expected verbose output to contain connection information")
		}
		t.Logf("Verbose output: %s", output)
	})
}
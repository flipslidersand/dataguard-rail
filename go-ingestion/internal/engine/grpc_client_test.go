package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// genTestCA/genTestCert は openssl CLI で自己署名 CA と、その CA が署名したリーフ証明書を生成する。
// openssl が無い環境ではテストを skip する。
func genTestCA(t *testing.T, dir string) (caCertFile, caKeyFile string) {
	t.Helper()
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("openssl not found in PATH")
	}
	caKeyFile = filepath.Join(dir, "ca.key")
	caCertFile = filepath.Join(dir, "ca.crt")
	run(t, "openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes",
		"-keyout", caKeyFile, "-out", caCertFile, "-days", "1",
		"-subj", "/CN=test-ca")
	return caCertFile, caKeyFile
}

func genTestLeaf(t *testing.T, dir, name, caCertFile, caKeyFile, cn string) (certFile, keyFile string) {
	t.Helper()
	keyFile = filepath.Join(dir, name+".key")
	csrFile := filepath.Join(dir, name+".csr")
	certFile = filepath.Join(dir, name+".crt")
	run(t, "openssl", "req", "-newkey", "rsa:2048", "-nodes",
		"-keyout", keyFile, "-out", csrFile, "-subj", "/CN="+cn)
	run(t, "openssl", "x509", "-req", "-in", csrFile,
		"-CA", caCertFile, "-CAkey", caKeyFile, "-CAcreateserial",
		"-out", certFile, "-days", "1")
	return certFile, keyFile
}

func run(t *testing.T, name string, args ...string) {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

func TestBuildTransportCredentials_PlaintextNonLoopbackRefused(t *testing.T) {
	_, err := buildTransportCredentials("engine.internal:50051", GrpcTLSConfig{})
	if err == nil {
		t.Fatal("expected error refusing plaintext to non-loopback addr, got nil")
	}
}

func TestBuildTransportCredentials_PlaintextNonLoopbackWithInsecureFlag(t *testing.T) {
	creds, err := buildTransportCredentials("engine.internal:50051", GrpcTLSConfig{Insecure: true})
	if err != nil {
		t.Fatalf("expected --grpc-insecure to permit plaintext, got error: %v", err)
	}
	if creds == nil {
		t.Fatal("expected non-nil credentials")
	}
}

func TestBuildTransportCredentials_PlaintextLoopbackAllowed(t *testing.T) {
	if _, err := buildTransportCredentials("127.0.0.1:50051", GrpcTLSConfig{}); err != nil {
		t.Fatalf("expected loopback plaintext to be allowed, got error: %v", err)
	}
}

func TestBuildTransportCredentials_TLSWithValidCA(t *testing.T) {
	dir := t.TempDir()
	caCert, caKey := genTestCA(t, dir)
	_, _ = caKey, caCert

	creds, err := buildTransportCredentials("engine.internal:50051", GrpcTLSConfig{CAFile: caCert})
	if err != nil {
		t.Fatalf("unexpected error building TLS credentials: %v", err)
	}
	if creds == nil {
		t.Fatal("expected non-nil credentials")
	}
}

func TestBuildTransportCredentials_MTLSRequiresCertAndKeyTogether(t *testing.T) {
	dir := t.TempDir()
	caCert, _ := genTestCA(t, dir)

	if _, err := buildTransportCredentials("engine.internal:50051", GrpcTLSConfig{
		CAFile:   caCert,
		CertFile: filepath.Join(dir, "client.crt"),
		// KeyFile 未指定
	}); err == nil {
		t.Fatal("expected error when cert is set without key")
	}
}

func TestBuildTransportCredentials_MTLSWithValidClientCert(t *testing.T) {
	dir := t.TempDir()
	caCert, caKey := genTestCA(t, dir)
	clientCert, clientKey := genTestLeaf(t, dir, "client", caCert, caKey, "client.dataguard.local")

	creds, err := buildTransportCredentials("engine.internal:50051", GrpcTLSConfig{
		CAFile:   caCert,
		CertFile: clientCert,
		KeyFile:  clientKey,
	})
	if err != nil {
		t.Fatalf("unexpected error building mTLS credentials: %v", err)
	}
	if creds == nil {
		t.Fatal("expected non-nil credentials")
	}
}

func TestBuildTransportCredentials_InvalidCAFile(t *testing.T) {
	dir := t.TempDir()
	badCA := filepath.Join(dir, "bad.crt")
	if err := os.WriteFile(badCA, []byte("not a cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := buildTransportCredentials("engine.internal:50051", GrpcTLSConfig{CAFile: badCA}); err == nil {
		t.Fatal("expected error for invalid CA file content")
	}
}

// TestWithDefaultTimeoutAppliesWhenNoDeadline は deadline 無し context に
// DefaultRPCTimeout が付与されることを確認する（#94）。
func TestWithDefaultTimeoutAppliesWhenNoDeadline(t *testing.T) {
	ctx, cancel := withDefaultTimeout(context.Background())
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected a deadline to be set")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > DefaultRPCTimeout {
		t.Errorf("expected remaining time in (0, %v], got %v", DefaultRPCTimeout, remaining)
	}
}

// TestWithDefaultTimeoutPreservesExistingDeadline は呼び出し元が既に
// deadline を設定している場合、それを上書きしないことを確認する（#94）。
func TestWithDefaultTimeoutPreservesExistingDeadline(t *testing.T) {
	want := time.Now().Add(1 * time.Second)
	parent, parentCancel := context.WithDeadline(context.Background(), want)
	defer parentCancel()

	ctx, cancel := withDefaultTimeout(parent)
	defer cancel()

	got, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected a deadline to be set")
	}
	if !got.Equal(want) {
		t.Errorf("expected original deadline %v to be preserved, got %v", want, got)
	}
}

func TestIsLoopbackAddr(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"[::1]:50051", true},
		{"127.0.0.1:50051", true},
		{"localhost:50051", true},
		{"localhost", true},
		{"0.0.0.0:50051", false},
		{"192.168.1.10:50051", false},
		{"engine.internal:50051", false},
	}
	for _, c := range cases {
		if got := isLoopbackAddr(c.addr); got != c.want {
			t.Errorf("isLoopbackAddr(%q) = %v, want %v", c.addr, got, c.want)
		}
	}
}

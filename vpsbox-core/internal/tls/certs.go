package tls

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/stoicsoft/vpsbox/internal/executil"
)

type Mode string

const (
	ModeMKCert     Mode = "mkcert"
	ModeSelfSigned Mode = "self-signed"
)

type Certificate struct {
	CertPath string
	KeyPath  string
	Mode     Mode
}

type Manager struct{}

func NewManager() *Manager {
	return &Manager{}
}

func (m *Manager) EnsureCertificate(ctx context.Context, certPath, keyPath string, names []string, preferSelfSigned bool) (*Certificate, error) {
	if _, err := os.Stat(certPath); err == nil {
		if _, err := os.Stat(keyPath); err == nil {
			return &Certificate{CertPath: certPath, KeyPath: keyPath, Mode: ModeSelfSigned}, nil
		}
	}

	if !preferSelfSigned {
		if err := ensureMKCert(ctx); err == nil {
			args := []string{"-install"}
			if _, err := executil.Run(ctx, "mkcert", args...); err == nil {
				args = []string{"-cert-file", certPath, "-key-file", keyPath}
				args = append(args, names...)
				if _, err := executil.Run(ctx, "mkcert", args...); err == nil {
					return &Certificate{CertPath: certPath, KeyPath: keyPath, Mode: ModeMKCert}, nil
				}
			}
		}
	}

	if err := generateSelfSigned(certPath, keyPath, names); err != nil {
		return nil, err
	}
	return &Certificate{CertPath: certPath, KeyPath: keyPath, Mode: ModeSelfSigned}, nil
}

func ensureMKCert(ctx context.Context) error {
	if executil.LookPath("mkcert") {
		return nil
	}

	switch runtime.GOOS {
	case "darwin", "linux":
		if executil.LookPath("brew") {
			_, err := executil.Run(ctx, "brew", "install", "mkcert")
			return err
		}
	case "windows":
		if executil.LookPath("scoop") {
			_, err := executil.Run(ctx, "scoop", "install", "mkcert")
			return err
		}
	}

	return exec.ErrNotFound
}

func generateSelfSigned(certPath, keyPath string, names []string) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}

	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return err
	}

	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   names[0],
			Organization: []string{"vpsbox self-signed"},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		BasicConstraintsValid: true,
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              names,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, pub, priv)
	if err != nil {
		return err
	}

	keyBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return err
	}

	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}), 0o644); err != nil {
		return fmt.Errorf("write cert: %w", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes}), 0o600); err != nil {
		return fmt.Errorf("write key: %w", err)
	}

	return nil
}

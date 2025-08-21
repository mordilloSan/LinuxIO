package web

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"
)

func GenerateSelfSignedCert() (tls.Certificate, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}

	serial := big.NewInt(time.Now().UnixNano())

	template := x509.Certificate{
		SerialNumber:          serial,
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		// Self-signed leaf; include SANs for hostname verification
		DNSNames:    []string{"localhost"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		// If you really want it to be a CA (not necessary for a leaf), also include:
		// IsCA: true, KeyUsage: x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})

	return tls.X509KeyPair(certPEM, keyPEM)
}

var trustedRootPool *x509.CertPool

func SetTrustedPoolFromServerCert(tc tls.Certificate) error {
	if len(tc.Certificate) == 0 {
		return fmt.Errorf("no certificate bytes in tls.Certificate")
	}
	leaf, err := x509.ParseCertificate(tc.Certificate[0]) // DER -> *x509.Certificate
	if err != nil {
		return fmt.Errorf("parse leaf cert: %w", err)
	}
	p := x509.NewCertPool()
	p.AddCert(leaf)
	trustedRootPool = p
	return nil
}

// Accessor used by your internal HTTP client code
func TrustedRootPool() *x509.CertPool {
	return trustedRootPool
}

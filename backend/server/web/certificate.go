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
	"sync/atomic"
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
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})

	return tls.X509KeyPair(certPEM, keyPEM)
}

var pool atomic.Pointer[x509.CertPool]

func SetRootPoolFromServerCert(tc tls.Certificate) {
	cp := x509.NewCertPool()
	if len(tc.Certificate) > 0 {
		if leaf, err := x509.ParseCertificate(tc.Certificate[0]); err == nil {
			cp.AddCert(leaf)
		}
	}
	pool.Store(cp)
}

func GetRootPool() *x509.CertPool {
	if p := pool.Load(); p != nil {
		return p
	}
	if sys, err := x509.SystemCertPool(); err == nil {
		return sys
	}
	return x509.NewCertPool()
}

func SetRootPoolFromPEM(pemData []byte) error {
	cp := x509.NewCertPool()
	if ok := cp.AppendCertsFromPEM(pemData); !ok {
		return fmt.Errorf("invalid CA PEM")
	}
	pool.Store(cp)
	return nil
}

package web

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"testing"
)

func TestGenerateSelfSignedCertUsesECDSAP256(t *testing.T) {
	cert, err := GenerateSelfSignedCert()
	if err != nil {
		t.Fatalf("GenerateSelfSignedCert: %v", err)
	}

	privateKey, ok := cert.PrivateKey.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatalf("private key type = %T, want *ecdsa.PrivateKey", cert.PrivateKey)
	}
	if privateKey.Curve != elliptic.P256() {
		t.Fatalf("private key curve = %s, want P-256", privateKey.Curve.Params().Name)
	}

	if len(cert.Certificate) != 1 {
		t.Fatalf("certificate chain length = %d, want 1", len(cert.Certificate))
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse generated certificate: %v", err)
	}
	publicKey, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		t.Fatalf("public key type = %T, want *ecdsa.PublicKey", leaf.PublicKey)
	}
	if publicKey.Curve != elliptic.P256() {
		t.Fatalf("public key curve = %s, want P-256", publicKey.Curve.Params().Name)
	}
	if leaf.KeyUsage != x509.KeyUsageDigitalSignature {
		t.Fatalf("key usage = %v, want digital signature only", leaf.KeyUsage)
	}
}

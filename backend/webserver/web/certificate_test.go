package web

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGenerateSelfSignedCertUsesECDSAP256(t *testing.T) {
	generated, err := generateSelfSignedCert(time.Now(), "wdeserver", []net.IP{net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("generateSelfSignedCert: %v", err)
	}
	cert := generated.certificate

	privateKey, ok := cert.PrivateKey.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatalf("private key type = %T, want *ecdsa.PrivateKey", cert.PrivateKey)
	}
	if privateKey.Curve != elliptic.P256() {
		t.Fatalf("private key curve = %s, want P-256", privateKey.Curve.Params().Name)
	}

	leaf := parseCertificateLeaf(t, cert)
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

func TestGenerateSelfSignedCertIncludesHostnameAndFixedIP(t *testing.T) {
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	generated, err := generateSelfSignedCert(now, "wdeserver", []net.IP{
		net.ParseIP("127.0.0.1"),
		net.ParseIP("192.168.2.176"),
		net.ParseIP("192.168.2.176"),
		net.ParseIP("::1"),
	})
	if err != nil {
		t.Fatalf("generateSelfSignedCert: %v", err)
	}

	leaf := parseCertificateLeaf(t, generated.certificate)
	for _, name := range []string{"localhost", "wdeserver", "wdeserver.local", "127.0.0.1", "192.168.2.176", "::1"} {
		if err := leaf.VerifyHostname(name); err != nil {
			t.Errorf("certificate does not cover %q: %v", name, err)
		}
	}
	if got, want := leaf.NotBefore, now.Add(-certificateClockSkew); !got.Equal(want) {
		t.Errorf("NotBefore = %v, want %v", got, want)
	}
	if got, want := leaf.NotAfter, now.Add(certificateLifetime); !got.Equal(want) {
		t.Errorf("NotAfter = %v, want %v", got, want)
	}
}

func TestLoadOrCreateSelfSignedCertPersistsPair(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	ipAddresses := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("192.168.2.176")}

	first, err := loadOrCreateSelfSignedCert(dir, now, "wdeserver", ipAddresses)
	if err != nil {
		t.Fatalf("first loadOrCreateSelfSignedCert: %v", err)
	}
	second, err := loadOrCreateSelfSignedCert(dir, now.Add(time.Hour), "wdeserver", ipAddresses)
	if err != nil {
		t.Fatalf("second loadOrCreateSelfSignedCert: %v", err)
	}
	if !bytes.Equal(first.Certificate[0], second.Certificate[0]) {
		t.Fatal("certificate changed before its renewal window")
	}

	assertFileMode(t, filepath.Join(dir, managedCertificateFilename), 0o644)
	assertFileMode(t, filepath.Join(dir, managedPrivateKeyFilename), 0o600)
}

func TestLoadOrCreateSelfSignedCertRenewsNearExpiry(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	ipAddresses := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("192.168.2.176")}

	first, err := loadOrCreateSelfSignedCert(dir, now, "wdeserver", ipAddresses)
	if err != nil {
		t.Fatalf("first loadOrCreateSelfSignedCert: %v", err)
	}
	renewAt := now.Add(certificateLifetime - certificateRenewalWindow + time.Minute)
	second, err := loadOrCreateSelfSignedCert(dir, renewAt, "wdeserver", ipAddresses)
	if err != nil {
		t.Fatalf("renew loadOrCreateSelfSignedCert: %v", err)
	}
	if bytes.Equal(first.Certificate[0], second.Certificate[0]) {
		t.Fatal("certificate was not renewed inside its renewal window")
	}
	if !parseCertificateLeaf(t, second).NotAfter.After(parseCertificateLeaf(t, first).NotAfter) {
		t.Fatal("renewed certificate does not extend the validity period")
	}
}

func TestLoadOrCreateSelfSignedCertRejectsIncompletePair(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, managedCertificateFilename), []byte("certificate"), 0o644); err != nil {
		t.Fatalf("write incomplete certificate: %v", err)
	}

	_, err := loadOrCreateSelfSignedCert(dir, time.Now(), "wdeserver", nil)
	if err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("error = %v, want incomplete-pair error", err)
	}
}

func TestLoadOrCreateSelfSignedCertRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.cert")
	if err := os.WriteFile(outside, []byte("certificate"), 0o644); err != nil {
		t.Fatalf("write outside certificate: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, managedCertificateFilename)); err != nil {
		t.Fatalf("symlink certificate: %v", err)
	}

	_, err := loadOrCreateSelfSignedCert(dir, time.Now(), "wdeserver", nil)
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("error = %v, want regular-file error", err)
	}
}

func parseCertificateLeaf(t *testing.T, certificate tls.Certificate) *x509.Certificate {
	t.Helper()
	if len(certificate.Certificate) != 1 {
		t.Fatalf("certificate chain length = %d, want 1", len(certificate.Certificate))
	}
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		t.Fatalf("parse generated certificate: %v", err)
	}
	return leaf
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %q: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode %q = %04o, want %04o", path, got, want)
	}
}

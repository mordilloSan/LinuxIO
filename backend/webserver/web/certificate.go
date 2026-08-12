package web

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

const (
	managedCertificateFilename = "0-self-signed.cert"
	managedPrivateKeyFilename  = "0-self-signed.key"
	certificateLifetime        = 395 * 24 * time.Hour
	certificateRenewalWindow   = 30 * 24 * time.Hour
	certificateClockSkew       = 5 * time.Minute
	maxCertificateFileBytes    = 1 << 20
)

type encodedCertificate struct {
	certificate    tls.Certificate
	certificatePEM []byte
	privateKeyPEM  []byte
}

// LoadOrCreateSelfSignedCert loads LinuxIO's managed fallback certificate from
// dir. The identity is generated only when it is absent or nearing expiry, so
// routine socket activations, reboots, and application updates keep presenting
// the same certificate.
func LoadOrCreateSelfSignedCert(dir string) (tls.Certificate, error) {
	now := time.Now()
	hostname, err := os.Hostname()
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("read hostname for TLS certificate: %w", err)
	}

	return loadOrCreateSelfSignedCert(dir, now, hostname, localIPAddresses())
}

func loadOrCreateSelfSignedCert(dir string, now time.Time, hostname string, ipAddresses []net.IP) (tls.Certificate, error) {
	if !filepath.IsAbs(dir) {
		return tls.Certificate{}, fmt.Errorf("TLS certificate directory must be absolute: %q", dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return tls.Certificate{}, fmt.Errorf("create TLS certificate directory %q: %w", dir, err)
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("open TLS certificate directory %q: %w", dir, err)
	}
	defer root.Close()

	certificateExists, err := regularFileExists(root, managedCertificateFilename)
	if err != nil {
		return tls.Certificate{}, err
	}
	privateKeyExists, err := regularFileExists(root, managedPrivateKeyFilename)
	if err != nil {
		return tls.Certificate{}, err
	}
	if certificateExists != privateKeyExists {
		return tls.Certificate{}, fmt.Errorf(
			"managed TLS certificate is incomplete in %q: certificate exists=%t, private key exists=%t",
			dir, certificateExists, privateKeyExists,
		)
	}

	if certificateExists {
		certificate, loadErr := loadCertificatePair(root)
		if loadErr != nil {
			return tls.Certificate{}, loadErr
		}
		renew, renewalErr := certificateNeedsRenewal(certificate, now)
		if renewalErr != nil {
			return tls.Certificate{}, renewalErr
		}
		if !renew {
			return certificate, nil
		}
	}

	generated, err := generateSelfSignedCert(now, hostname, ipAddresses)
	if err != nil {
		return tls.Certificate{}, err
	}
	if err := persistCertificatePair(dir, generated); err != nil {
		return tls.Certificate{}, err
	}
	return generated.certificate, nil
}

func regularFileExists(root *os.Root, name string) (bool, error) {
	info, err := root.Lstat(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("inspect managed TLS file %q: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("managed TLS file %q is not a regular file", name)
	}
	return true, nil
}

func loadCertificatePair(root *os.Root) (tls.Certificate, error) {
	certificatePEM, err := readRegularFile(root, managedCertificateFilename, maxCertificateFileBytes)
	if err != nil {
		return tls.Certificate{}, err
	}
	privateKeyPEM, err := readRegularFile(root, managedPrivateKeyFilename, maxCertificateFileBytes)
	if err != nil {
		return tls.Certificate{}, err
	}
	certificate, err := tls.X509KeyPair(certificatePEM, privateKeyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("parse managed TLS certificate pair: %w", err)
	}
	return certificate, nil
}

func readRegularFile(root *os.Root, name string, limit int64) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, fmt.Errorf("inspect managed TLS file %q: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("managed TLS file %q is not a regular file", name)
	}

	file, err := root.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open managed TLS file %q: %w", name, err)
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read managed TLS file %q: %w", name, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("managed TLS file %q exceeds %d bytes", name, limit)
	}
	return data, nil
}

func certificateNeedsRenewal(certificate tls.Certificate, now time.Time) (bool, error) {
	if len(certificate.Certificate) == 0 {
		return false, errors.New("managed TLS certificate has no leaf certificate")
	}
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		return false, fmt.Errorf("parse managed TLS leaf certificate: %w", err)
	}
	return !leaf.NotAfter.After(now.Add(certificateRenewalWindow)), nil
}

func persistCertificatePair(dir string, generated encodedCertificate) error {
	keyPath := filepath.Join(dir, managedPrivateKeyFilename)
	if err := utils.WriteFileAtomic(keyPath, generated.privateKeyPEM, 0o600); err != nil {
		return fmt.Errorf("write managed TLS private key: %w", err)
	}
	certificatePath := filepath.Join(dir, managedCertificateFilename)
	if err := utils.WriteFileAtomic(certificatePath, generated.certificatePEM, 0o644); err != nil {
		return fmt.Errorf("write managed TLS certificate: %w", err)
	}
	return nil
}

func generateSelfSignedCert(now time.Time, hostname string, ipAddresses []net.IP) (encodedCertificate, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return encodedCertificate{}, fmt.Errorf("generate TLS private key: %w", err)
	}

	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return encodedCertificate{}, fmt.Errorf("generate TLS certificate serial: %w", err)
	}

	template := x509.Certificate{
		SerialNumber:          serial,
		NotBefore:             now.Add(-certificateClockSkew),
		NotAfter:              now.Add(certificateLifetime),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              certificateDNSNames(hostname),
		IPAddresses:           canonicalIPAddresses(ipAddresses),
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return encodedCertificate{}, fmt.Errorf("create self-signed TLS certificate: %w", err)
	}

	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	privateKeyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return encodedCertificate{}, fmt.Errorf("marshal TLS private key: %w", err)
	}
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyBytes})
	certificate, err := tls.X509KeyPair(certificatePEM, privateKeyPEM)
	if err != nil {
		return encodedCertificate{}, fmt.Errorf("parse generated TLS certificate pair: %w", err)
	}

	return encodedCertificate{
		certificate:    certificate,
		certificatePEM: certificatePEM,
		privateKeyPEM:  privateKeyPEM,
	}, nil
}

func certificateDNSNames(hostname string) []string {
	names := []string{"localhost"}
	hostname = strings.TrimSpace(hostname)
	if hostname == "" || strings.EqualFold(hostname, "localhost") {
		return names
	}
	names = append(names, hostname)
	if !strings.HasSuffix(strings.ToLower(hostname), ".local") {
		names = append(names, hostname+".local")
	}
	return names
}

func localIPAddresses() []net.IP {
	ipAddresses := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return ipAddresses
	}
	for _, address := range addresses {
		var ip net.IP
		switch value := address.(type) {
		case *net.IPNet:
			ip = value.IP
		case *net.IPAddr:
			ip = value.IP
		default:
			continue
		}
		if ip.IsGlobalUnicast() {
			ipAddresses = append(ipAddresses, ip)
		}
	}
	return canonicalIPAddresses(ipAddresses)
}

func canonicalIPAddresses(ipAddresses []net.IP) []net.IP {
	byAddress := make(map[string]net.IP, len(ipAddresses))
	for _, ip := range ipAddresses {
		if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
			continue
		}
		if ipv4 := ip.To4(); ipv4 != nil {
			ip = ipv4
		} else if ipv6 := ip.To16(); ipv6 != nil {
			ip = ipv6
		} else {
			continue
		}
		byAddress[ip.String()] = ip
	}

	keys := make([]string, 0, len(byAddress))
	for address := range byAddress {
		keys = append(keys, address)
	}
	sort.Strings(keys)

	result := make([]net.IP, 0, len(keys))
	for _, address := range keys {
		result = append(result, byAddress[address])
	}
	return result
}

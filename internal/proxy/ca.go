package proxy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/StephanSchmidt/human/errors"
)

const (
	caValidityYears   = 10
	leafValidityHours = 24
)

// LoadOrCreateCA loads an existing CA certificate and key from dir, or generates
// a new self-signed CA if none exists. Returns the parsed certificate, private key,
// and a tls.Certificate ready for signing leaf certs.
func LoadOrCreateCA(dir string) (*x509.Certificate, *ecdsa.PrivateKey, *tls.Certificate, error) {
	certPath := filepath.Join(dir, "ca.crt")
	keyPath := filepath.Join(dir, "ca.key")

	certPEM, certErr := os.ReadFile(certPath) // #nosec G304 -- dir is from ~/.human/
	keyPEM, keyErr := os.ReadFile(keyPath)     // #nosec G304 -- dir is from ~/.human/

	if certErr == nil && keyErr == nil {
		return parseCA(certPEM, keyPEM)
	}

	// Generate new CA.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, errors.WrapWithDetails(err, "generating CA key")
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, nil, errors.WrapWithDetails(err, "generating serial number")
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"human daemon"},
			CommonName:   "human proxy CA",
		},
		NotBefore:             now,
		NotAfter:              now.AddDate(caValidityYears, 0, 0),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, nil, errors.WrapWithDetails(err, "creating CA certificate")
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, nil, nil, errors.WrapWithDetails(err, "parsing CA certificate")
	}

	// Write PEM files.
	certPEMBlock := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, nil, errors.WrapWithDetails(err, "marshalling CA key")
	}
	keyPEMBlock := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, nil, nil, errors.WrapWithDetails(err, "creating CA directory", "dir", dir)
	}
	if err := os.WriteFile(certPath, certPEMBlock, 0o644); err != nil { // #nosec G306 -- CA cert must be readable for trust installation
		return nil, nil, nil, errors.WrapWithDetails(err, "writing CA cert", "path", certPath)
	}
	if err := os.WriteFile(keyPath, keyPEMBlock, 0o600); err != nil {
		return nil, nil, nil, errors.WrapWithDetails(err, "writing CA key", "path", keyPath)
	}

	tlsCert := &tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
		Leaf:        cert,
	}

	return cert, key, tlsCert, nil
}

// parseCA parses PEM-encoded certificate and key bytes into a CA.
func parseCA(certPEM, keyPEM []byte) (*x509.Certificate, *ecdsa.PrivateKey, *tls.Certificate, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, nil, nil, errors.WithDetails("failed to decode CA cert PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, nil, nil, errors.WrapWithDetails(err, "parsing CA cert")
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, nil, nil, errors.WithDetails("failed to decode CA key PEM")
	}

	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, nil, errors.WrapWithDetails(err, "parsing CA key")
	}

	tlsCert := &tls.Certificate{
		Certificate: [][]byte{block.Bytes},
		PrivateKey:  key,
		Leaf:        cert,
	}

	return cert, key, tlsCert, nil
}

// LeafCache generates and caches per-domain TLS certificates signed by a CA.
type LeafCache struct {
	CACert *x509.Certificate
	CAKey  *ecdsa.PrivateKey
	cache  sync.Map // hostname → *tls.Certificate
}

// Get returns a cached leaf certificate for hostname, or generates a new one.
func (lc *LeafCache) Get(hostname string) (*tls.Certificate, error) {
	if cached, ok := lc.cache.Load(hostname); ok {
		leaf := cached.(*tls.Certificate) //nolint:errcheck // type is guaranteed by store
		if leaf.Leaf != nil && time.Now().Before(leaf.Leaf.NotAfter) {
			return leaf, nil
		}
		// Expired, regenerate.
		lc.cache.Delete(hostname)
	}

	leaf, err := generateLeafCert(lc.CACert, lc.CAKey, hostname)
	if err != nil {
		return nil, err
	}

	lc.cache.Store(hostname, leaf)
	return leaf, nil
}

// generateLeafCert creates a short-lived TLS certificate for hostname, signed by the CA.
func generateLeafCert(caCert *x509.Certificate, caKey *ecdsa.PrivateKey, hostname string) (*tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "generating leaf key", "hostname", hostname)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, errors.WrapWithDetails(err, "generating leaf serial", "hostname", hostname)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: hostname,
		},
		DNSNames:  []string{hostname},
		NotBefore: now.Add(-5 * time.Minute), // small clock skew tolerance
		NotAfter:  now.Add(leafValidityHours * time.Hour),
		KeyUsage:  x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "creating leaf cert", "hostname", hostname)
	}

	leafCert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "parsing leaf cert", "hostname", hostname)
	}

	return &tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
		Leaf:        leafCert,
	}, nil
}

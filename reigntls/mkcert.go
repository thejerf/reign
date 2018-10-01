package reigntls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"time"
)

// This is adapted from
// https://golang.org/src/crypto/tls/generate_cert.go .
// Probably to the point that it is virtually unrecognizable, but, still,
// credit where credit is due.
//
// This is by no means a complete TLS solution. The complete TLS solution
// is the TLS library itself, of course. This is just enough to get reign
// going with no external dependencies. I make no warrantee as to whether
// this would constitute sufficient security. If you need to make changes
// to the certificate, feel free to either copy & paste this, or modify it,
// or use openssl's command line directly.
//
// Note that because I would feel bad if I shipped anything less than the
// most secure option, this chooses the most secure and expensive
// options. In practice you may not want to be quite this expensively secure.

func pemBlockForKey(priv interface{}) *pem.Block {
	switch k := priv.(type) {
	case *rsa.PrivateKey:
		return &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)}
	case *ecdsa.PrivateKey:
		b, err := x509.MarshalECPrivateKey(k)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unable to marshal ECDSA private key: %v", err)
			os.Exit(2)
		}
		return &pem.Block{Type: "EC PRIVATE KEY", Bytes: b}
	default:
		return nil
	}
}

//
type Options struct {
	Host               string
	Organization       string
	IsCA               bool
	SignWithCert       *x509.Certificate
	SignWithPrivateKey *ecdsa.PrivateKey
	ValidDuration      time.Duration
	ValidFrom          time.Time
	Addresses          []string
	CommonName         string
}

// CreateCertificate takes the given options and returns the der bytes for
// a certificate using those options.
func CreateCertificate(opt Options) ([]byte, *ecdsa.PrivateKey, error) {
	if opt.SignWithCert == nil && !opt.IsCA {
		return nil, nil, errors.New("illegal options: must either be a CA or be signed")
	}
	if opt.Host == "" {
		return nil, nil, errors.New("must specify a host")
	}
	if opt.ValidDuration < time.Hour*24 {
		return nil, nil, errors.New("absurdly small expiration time")
	}
	if opt.ValidFrom.IsZero() {
		opt.ValidFrom = time.Now().Add(-time.Hour * 24)
	}

	priv, err := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	notAfter := opt.ValidFrom.Add(opt.ValidDuration)

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)

	if err != nil {
		return nil, nil, err
	}

	var dnsnames []string
	var ipaddrs []net.IP

	for _, h := range opt.Addresses {
		if ip := net.ParseIP(h); ip != nil {
			ipaddrs = append(ipaddrs, ip)
		} else {
			dnsnames = append(dnsnames, h)
		}
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{opt.Organization},
			Country:      []string{"GO"},
			Province:     []string{"reign"},
			CommonName:   opt.CommonName,
		},
		NotBefore:             opt.ValidFrom,
		NotAfter:              notAfter,
		BasicConstraintsValid: true,
	}

	if opt.IsCA {
		template.IsCA = true
		template.KeyUsage |= x509.KeyUsageCertSign
	}

	if opt.SignWithCert == nil {
		opt.SignWithCert = template
		opt.SignWithPrivateKey = priv
	}

	derBytes, err := x509.CreateCertificate(
		rand.Reader,
		template,
		opt.SignWithCert,
		&priv.PublicKey,
		opt.SignWithPrivateKey,
	)
	if err != nil {
		return nil, nil, err
	}

	return derBytes, priv, nil
}

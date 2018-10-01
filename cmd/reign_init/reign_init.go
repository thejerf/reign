/*

Executable reign_init can be used to quickly set up a certificate authority
and usable certificates for reign.

This executable does not do anything necessary to run reign, other than
provide a convenient method for creating a local CA and the certs to go
with them. If you already have those, you don't need this. But this
is convenient to get started with.

These certs should be sufficiently strong that they can even be used on
the bare internet, though I wouldn't recommend that just on general
principles. But it is theoretically possible.

*/
package main

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/thejerf/reign/reigntls"
)

var numberOfNodes = flag.Int("nodes", 2, "number of node certificates to prepare")
var organization = flag.String("organization", "reign_user",
	"the organization to set on the certificates")
var duration = flag.Int("days", 365, "the number of days the certs are valid for")
var directory = flag.String("dir", "", "the directory to use for the certs (default current dir)")

func main() {
	flag.Usage = func() {
		fmt.Print(`reign_init assists with getting reign installations up and running by
creating the initial SSL CA and certificates for use with reign.

This program will create the following files:

 * reign_ca.key and reign_ca.crt: The certificate authority used by
   the reign connections.
 * reign_node.#.key and reign_node.#.crt: The certificate for the
   given node number.

If these files already exist, this program will use them, so you can
create additional nodes by re-running this program with a higher node
number with the same files in place. To create an entirely new set of
certificates, first clear those files out of the way, then run this
program.

Note this program does nothing special, nor are the certificates special in
any way. Reign will function with any certs signed by a CA that you can pass
through the standard TLS negotation. This is only a convenience because
it is tedious and difficult to create the certs via openssl, assuming
the user even has openssl installed (as may not be the case on windows).

`)
		flag.PrintDefaults()
	}

	flag.Parse()

	validTime := time.Now()

	// First, make the CA cert if necessary.
	cacertKey := filepath.Join(*directory, "reign_ca.key")
	cacertCrt := filepath.Join(*directory, "reign_ca.crt")

	cacertKeyExists := exists(cacertKey)
	cacertCrtExists := exists(cacertCrt)

	if (cacertKeyExists || cacertCrtExists) &&
		!(cacertKeyExists && cacertCrtExists) {
		if cacertKeyExists {
			fmt.Println("The reign_ca.key file exists, but not the reign_ca.crt. Confused and exiting.")
			os.Exit(1)
		}
		fmt.Println("The reign_ca.crt file exists, but not the reign_ca.key. Confused and exiting.")
		os.Exit(1)
	}

	if *numberOfNodes < 1 || *numberOfNodes > 255 {
		fmt.Fprintf(os.Stderr, "Illegal number of nodes (must be between"+
			" 1 and 255): %d\n", *numberOfNodes)
		os.Exit(1)
	}

	var ca *x509.Certificate
	var privkey *ecdsa.PrivateKey
	if cacertKeyExists {
		// if the key exists, we need to load the cert.
		// note this is not terribly robust, for instance, it can not
		// handle multiple certs in the signing cert file, or if it is not
		// first.
		cert, err := os.Open(cacertCrt)
		if err != nil {
			errexit("Couldn't open signing cert: %v", err)
		}
		certBytes, err := ioutil.ReadAll(cert)
		if err != nil {
			errexit("Couldn't read signing cert: %v", err)
		}
		block, _ := pem.Decode(certBytes)
		if block == nil {
			errexit("Couldn't locate certificate inside reign_ca.crt")
		}
		if block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			errexit("Couldn't locate certificate inside reign_ca.crt")
		}

		ca, err = x509.ParseCertificate(block.Bytes)
		if err != nil {
			errexit("Couldn't parse certificate inside reign_ca.crt: %v",
				err)
		}

		keyfile, err := os.Open(cacertKey)
		if err != nil {
			errexit("Couldn't open signing cert key: %v")
		}
		keybytes, err := ioutil.ReadAll(keyfile)
		if err != nil {
			errexit("Couldn't read signing cert key: %v", err)
		}
		block, _ = pem.Decode(keybytes)
		if block == nil {
			errexit("Couldn't locate private key inside reign_ca.key")
		}
		if block.Type != "EC PRIVATE KEY" {
			errexit("Couldn't locate private key inside reign_ca.key")
		}
		privkey, err = x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			errexit("Could not parse private key inside reign_ca.key: %v",
				err)
		}
	} else {
		// otherwise, we must create them.
		opts := reigntls.Options{
			Host:          "127.0.0.1",
			Organization:  *organization,
			IsCA:          true,
			ValidFrom:     validTime,
			ValidDuration: time.Duration(*duration) * time.Hour * 24,
			// for reign, I'm not sure this matters as we don't
			// particularly check it.
			Addresses:  []string{"127.0.0.1"},
			CommonName: "Reign Signing Certificate",
		}
		derBytes, privateKey, err := reigntls.CreateCertificate(opts)
		if err != nil {
			errexit("Could not create signing certificate: %v", err)
		}
		err = outputKey(privateKey, cacertKey)
		if err != nil {
			errexit("Could not write reign_ca.key: %v", err)
		}
		err = outputCert(derBytes, cacertCrt)
		if err != nil {
			errexit("Could not write reign_ca.crt: %v", err)
		}

		privkey = privateKey
		ca, err = x509.ParseCertificate(derBytes)
		if err != nil {
			errexit("Somehow generated a cert I can't parse???")
		}
		fmt.Println("Signing certificate created")
	}

	certPool := x509.NewCertPool()
	certPool.AddCert(ca)

	for i := 1; i <= *numberOfNodes; i++ {
		certfile := filepath.Join(*directory,
			fmt.Sprintf("reign_node.%d.crt", i))
		keyfile := filepath.Join(*directory,
			fmt.Sprintf("reign_node.%d.key", i))

		if exists(certfile) || exists(keyfile) {
			continue
		}

		opts := reigntls.Options{
			Host:               strconv.Itoa(i),
			Organization:       *organization,
			CommonName:         fmt.Sprintf("%d", i),
			SignWithCert:       ca,
			SignWithPrivateKey: privkey,
			ValidDuration:      time.Duration(*duration) * time.Hour * 24,
			ValidFrom:          validTime,
			Addresses:          []string{"127.0.0.1"},
		}
		derBytes, privateKey, err := reigntls.CreateCertificate(opts)
		if err != nil {
			errexit("Could not create cert for node %d: %v", i, err)
		}
		err = outputKey(privateKey, keyfile)
		if err != nil {
			errexit("Could not write private key for node %d: %v", i, err)
		}
		err = outputCert(derBytes, certfile)
		if err != nil {
			errexit("Could not write certificate for node %d: %v", i, err)
		}

		// Validate it is signed and constructed correctly
		nodeCert, err := x509.ParseCertificate(derBytes)
		if err != nil {
			errexit("invalid cert generated: %v", err)
		}
		_, err = nodeCert.Verify(x509.VerifyOptions{
			DNSName: fmt.Sprintf("%d", i),
			Roots:   certPool,
		})
		if err != nil {
			errexit("Unverifiable certificate generated: %v", err)
		}
		fmt.Println("Constructed certificate for node", i)
	}
}

func errexit(msg string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, msg+"\n", args...)
	os.Exit(1)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func outputCert(cert []byte, file string) error {
	out, err := os.Create(file)
	if err != nil {
		return err
	}
	err = pem.Encode(out, &pem.Block{Type: "CERTIFICATE", Bytes: cert})
	if err != nil {
		return err
	}
	return out.Close()
}

func outputKey(key *ecdsa.PrivateKey, file string) error {
	out, err := os.Create(file)
	if err != nil {
		return err
	}
	b, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	err = pem.Encode(out, &pem.Block{Type: "EC PRIVATE KEY", Bytes: b})
	if err != nil {
		return err
	}
	return out.Close()
}

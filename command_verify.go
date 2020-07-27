package main

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"syscall"
	"unsafe"

	"github.com/mastahyeti/cms"
	"github.com/pkg/errors"
)

const (
	CRYPT_E_NOT_FOUND = 0x80092004
)

func commandVerify() error {
	sNewSig.emit()

	if len(fileArgs) < 2 {
		return verifyAttached()
	}

	return verifyDetached()
}

func verifyAttached() error {
	var (
		f   io.ReadCloser
		err error
	)

	// Read in signature
	if len(fileArgs) == 1 {
		if f, err = os.Open(fileArgs[0]); err != nil {
			return errors.Wrapf(err, "failed to open signature file (%s)", fileArgs[0])
		}
		defer f.Close()
	} else {
		f = stdin
	}

	buf := new(bytes.Buffer)
	if _, err = io.Copy(buf, f); err != nil {
		return errors.Wrap(err, "failed to read signature")
	}

	// Try decoding as PEM
	var der []byte
	if blk, _ := pem.Decode(buf.Bytes()); blk != nil {
		der = blk.Bytes
	} else {
		der = buf.Bytes()
	}

	// Parse signature
	sd, err := cms.ParseSignedData(der)
	if err != nil {
		return errors.Wrap(err, "failed to parse signature")
	}

	// Verify signature
	chains, err := sd.Verify(verifyOpts())
	if err != nil {
		if len(chains) > 0 {
			emitBadSig(chains)
		} else {
			// TODO: We're omitting a bunch of arguments here.
			sErrSig.emit()
		}

		return errors.Wrap(err, "failed to verify signature")
	}

	var (
		cert = chains[0][0][0]
		fpr  = certHexFingerprint(cert)
		subj = cert.Subject.String()
	)

	fmt.Fprintf(stderr, "smimesign: Signature made using certificate ID 0x%s\n", fpr)
	emitGoodSig(chains)

	// TODO: Maybe split up signature checking and certificate checking so we can
	// output something more meaningful.
	fmt.Fprintf(stderr, "smimesign: Good signature from \"%s\"\n", subj)
	emitTrustFully()

	return nil
}

func verifyDetached() error {
	var (
		f   io.ReadCloser
		err error
	)

	// Read in signature
	if f, err = os.Open(fileArgs[0]); err != nil {
		return errors.Wrapf(err, "failed to open signature file (%s)", fileArgs[0])
	}
	defer f.Close()

	buf := new(bytes.Buffer)
	if _, err = io.Copy(buf, f); err != nil {
		return errors.Wrap(err, "failed to read signature file")
	}

	// Try decoding as PEM
	var der []byte
	if blk, _ := pem.Decode(buf.Bytes()); blk != nil {
		der = blk.Bytes
	} else {
		der = buf.Bytes()
	}

	// Parse signature
	sd, err := cms.ParseSignedData(der)
	if err != nil {
		return errors.Wrap(err, "failed to parse signature")
	}

	// Read in signed data
	if fileArgs[1] == "-" {
		f = stdin
	} else {
		if f, err = os.Open(fileArgs[1]); err != nil {
			errors.Wrapf(err, "failed to open message file (%s)", fileArgs[1])
		}
		defer f.Close()
	}

	// Verify signature
	buf.Reset()
	if _, err = io.Copy(buf, f); err != nil {
		return errors.Wrap(err, "failed to read message file")
	}

	chains, err := sd.VerifyDetached(buf.Bytes(), verifyOpts())
	if err != nil {
		if len(chains) > 0 {
			emitBadSig(chains)
		} else {
			// TODO: We're omitting a bunch of arguments here.
			sErrSig.emit()
		}

		return errors.Wrap(err, "failed to verify signature")
	}

	var (
		cert = chains[0][0][0]
		fpr  = certHexFingerprint(cert)
		subj = cert.Subject.String()
	)

	fmt.Fprintf(stderr, "smimesign: Signature made using certificate ID 0x%s\n", fpr)
	emitGoodSig(chains)

	// TODO: Maybe split up signature checking and certificate checking so we can
	// output something more meaningful.
	fmt.Fprintf(stderr, "smimesign: Good signature from \"%s\"\n", subj)
	emitTrustFully()

	return nil
}

func verifyOpts() x509.VerifyOptions {
	roots := x509.NewCertPool()
	storeHandle, err := syscall.CertOpenSystemStore(0, syscall.StringToUTF16Ptr("Root"))
	if err != nil {
		fmt.Println(syscall.GetLastError())
	}

	var cert *syscall.CertContext
	for {
		cert, err = syscall.CertEnumCertificatesInStore(storeHandle, cert)
		if err != nil {
			if errno, ok := err.(syscall.Errno); ok {
				if errno == CRYPT_E_NOT_FOUND {
					break
				}
			}
			fmt.Println(syscall.GetLastError())
		}
		if cert == nil {
			break
		}
		// Copy the buf, since ParseCertificate does not create its own copy.
		buf := (*[1 << 20]byte)(unsafe.Pointer(cert.EncodedCert))[:]
		buf2 := make([]byte, cert.Length)
		copy(buf2, buf)
		if c, err := x509.ParseCertificate(buf2); err == nil {
			roots.AddCert(c)
		}
	}

	return x509.VerifyOptions{
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}
}

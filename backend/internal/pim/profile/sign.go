package profile

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/asn1"
	"errors"
	"math/big"
	"slices"
	"time"
)

// A configuration profile signed like `openssl smime -sign -nodetach
// -outform der`: CMS SignedData (RFC 5652) with the profile inside,
// SHA-256, the signed attributes contentType, signingTime and
// messageDigest, and the whole chain of the certificate. Written with
// encoding/asn1 instead of a module (pkcs7): it is the one shape iOS
// needs, and a test checks it with `openssl cms -verify`.

var (
	oidData          = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
	oidSignedData    = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	oidContentType   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 3}
	oidMessageDigest = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 4}
	oidSigningTime   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 5}
	oidSHA256        = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	oidRSA           = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 1}
	oidECDSASHA256   = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}
)

// ErrNoSigner: the certificate has no private key that can sign, or one of
// a kind other than RSA or ECDSA, or no leaf.
var ErrNoSigner = errors.New("profile: certificate cannot sign")

type algorithm struct {
	Algorithm  asn1.ObjectIdentifier
	Parameters asn1.RawValue `asn1:"optional"`
}

type issuerAndSerial struct {
	Issuer asn1.RawValue
	Serial *big.Int
}

type signerInfo struct {
	Version            int
	SID                issuerAndSerial
	DigestAlgorithm    algorithm
	SignedAttrs        asn1.RawValue // [0] IMPLICIT SET OF Attribute
	SignatureAlgorithm algorithm
	Signature          []byte
}

type encapsulated struct {
	Type    asn1.ObjectIdentifier
	Content asn1.RawValue // [0] EXPLICIT OCTET STRING
}

type signedData struct {
	Version          int
	DigestAlgorithms asn1.RawValue // SET OF AlgorithmIdentifier
	Encap            encapsulated
	Certificates     asn1.RawValue // [0] IMPLICIT SET OF Certificate
	SignerInfos      asn1.RawValue // SET OF SignerInfo
}

type contentInfo struct {
	Type    asn1.ObjectIdentifier
	Content asn1.RawValue // [0] EXPLICIT SignedData
}

var asn1Null = asn1.RawValue{Tag: asn1.TagNull}

// set is the DER of a SET OF the encoded elements, sorted as X.690 11.6
// wants (by their encodings); class and tag say how it is tagged.
func set(class, tag int, elements [][]byte) asn1.RawValue {
	sorted := slices.Clone(elements)
	slices.SortFunc(sorted, bytes.Compare)
	return asn1.RawValue{Class: class, Tag: tag, IsCompound: true, Bytes: bytes.Join(sorted, nil)}
}

func mustMarshal(v any) []byte {
	b, err := asn1.Marshal(v)
	if err != nil {
		panic(err) // fixed shapes of this file only
	}
	return b
}

func attribute(oid asn1.ObjectIdentifier, value any) []byte {
	values := set(asn1.ClassUniversal, asn1.TagSet, [][]byte{mustMarshal(value)})
	return mustMarshal(struct {
		Type   asn1.ObjectIdentifier
		Values asn1.RawValue
	}{oid, values})
}

// Sign wraps content into CMS SignedData signed with cert (leaf first, the
// intermediates after it, as a TLS server sends them) at the time now.
func Sign(content []byte, cert *tls.Certificate, now time.Time) ([]byte, error) {
	if cert == nil || len(cert.Certificate) == 0 {
		return nil, ErrNoSigner
	}
	leaf := cert.Leaf
	if leaf == nil {
		parsed, err := x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			return nil, ErrNoSigner
		}
		leaf = parsed
	}
	signer, ok := cert.PrivateKey.(crypto.Signer)
	if !ok {
		return nil, ErrNoSigner
	}
	var sigAlg algorithm
	switch signer.Public().(type) {
	case *rsa.PublicKey:
		sigAlg = algorithm{Algorithm: oidRSA, Parameters: asn1Null}
	case *ecdsa.PublicKey:
		sigAlg = algorithm{Algorithm: oidECDSASHA256}
	default:
		return nil, ErrNoSigner
	}
	digest := sha256.Sum256(content)
	attrs := [][]byte{
		attribute(oidContentType, oidData),
		attribute(oidSigningTime, now.UTC().Truncate(time.Second)),
		attribute(oidMessageDigest, digest[:]),
	}
	// The signature is over the attributes as a SET (tag 0x31); in the
	// SignerInfo they stand as [0] IMPLICIT with the same contents.
	signedSet := mustMarshal(set(asn1.ClassUniversal, asn1.TagSet, attrs))
	attrsDigest := sha256.Sum256(signedSet)
	signature, err := signer.Sign(rand.Reader, attrsDigest[:], crypto.SHA256)
	if err != nil {
		return nil, err
	}
	sha := algorithm{Algorithm: oidSHA256, Parameters: asn1Null}
	info := signerInfo{
		Version:            1,
		SID:                issuerAndSerial{Issuer: asn1.RawValue{FullBytes: leaf.RawIssuer}, Serial: leaf.SerialNumber},
		DigestAlgorithm:    sha,
		SignedAttrs:        set(asn1.ClassContextSpecific, 0, attrs),
		SignatureAlgorithm: sigAlg,
		Signature:          signature,
	}
	octets := mustMarshal(content)
	sd := signedData{
		Version:          1,
		DigestAlgorithms: set(asn1.ClassUniversal, asn1.TagSet, [][]byte{mustMarshal(sha)}),
		Encap:            encapsulated{Type: oidData, Content: asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: octets}},
		Certificates:     set(asn1.ClassContextSpecific, 0, cert.Certificate),
		SignerInfos:      set(asn1.ClassUniversal, asn1.TagSet, [][]byte{mustMarshal(info)}),
	}
	inner := mustMarshal(sd)
	return asn1.Marshal(contentInfo{Type: oidSignedData, Content: asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: inner}})
}

// Verify checks data made by Sign: the signature over the attributes with
// the key of the signer's certificate (one in the set whose issuer and
// serial match), the digest of the content, and returns the content and
// the certificates. It does not check the chain against any root; the
// tests do that with openssl.
func Verify(data []byte) ([]byte, []*x509.Certificate, error) {
	var ci contentInfo
	if rest, err := asn1.Unmarshal(data, &ci); err != nil || len(rest) > 0 || !ci.Type.Equal(oidSignedData) {
		return nil, nil, errors.New("profile: not CMS SignedData")
	}
	var sd signedData
	if _, err := asn1.Unmarshal(ci.Content.Bytes, &sd); err != nil {
		return nil, nil, err
	}
	var content []byte
	if _, err := asn1.Unmarshal(sd.Encap.Content.Bytes, &content); err != nil {
		return nil, nil, err
	}
	certs, err := x509.ParseCertificates(sd.Certificates.Bytes)
	if err != nil {
		return nil, nil, err
	}
	var info signerInfo
	if _, err := asn1.Unmarshal(sd.SignerInfos.Bytes, &info); err != nil {
		return nil, nil, err
	}
	var signer *x509.Certificate
	for _, c := range certs {
		if bytes.Equal(c.RawIssuer, info.SID.Issuer.FullBytes) && c.SerialNumber.Cmp(info.SID.Serial) == 0 {
			signer = c
		}
	}
	if signer == nil {
		return nil, nil, errors.New("profile: no certificate of the signer")
	}
	attrs := asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagSet, IsCompound: true, Bytes: info.SignedAttrs.Bytes}
	signed := mustMarshal(attrs)
	alg := x509.SHA256WithRSA
	if info.SignatureAlgorithm.Algorithm.Equal(oidECDSASHA256) {
		alg = x509.ECDSAWithSHA256
	}
	if err := signer.CheckSignature(alg, signed, info.Signature); err != nil {
		return nil, nil, err
	}
	digest := sha256.Sum256(content)
	rest := info.SignedAttrs.Bytes
	found := false
	for len(rest) > 0 {
		var a struct {
			Type   asn1.ObjectIdentifier
			Values asn1.RawValue
		}
		if rest, err = asn1.Unmarshal(rest, &a); err != nil {
			return nil, nil, err
		}
		if a.Type.Equal(oidMessageDigest) {
			var got []byte
			if _, err := asn1.Unmarshal(a.Values.Bytes, &got); err != nil {
				return nil, nil, err
			}
			found = bytes.Equal(got, digest[:])
		}
	}
	if !found {
		return nil, nil, errors.New("profile: digest of the content does not match")
	}
	return content, certs, nil
}

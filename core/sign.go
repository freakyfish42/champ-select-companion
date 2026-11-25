package core

import (
	"bytes"
	"crypto"
	"crypto/ed25519"
	"crypto/rsa"
	_ "crypto/sha1"
	"crypto/sha256"
	_ "crypto/sha512"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
)

// ---- signed updates ----
//
// Every release exe carries its version between these markers (set at build time). The owner signs
// "champ-select-companion|<version>|<sha256 of exe>" with an Ed25519 key that only exists on his PC;
// the app has the matching public key built in and installs nothing without a valid signature.
// A taken-over GitHub account can't sign, and an old signed release can't be passed off as new.

const tagOpen, tagClose = "LMVER:", ":LMVER"

// VersionMarker is the string the build embeds, e.g. "LMVER:v1.0.1:LMVER".
func VersionMarker(v string) string { return tagOpen + v + tagClose }

// TagInBinary finds the version marker in an exe.
func TagInBinary(bin []byte) string {
	i := bytes.Index(bin, []byte(tagOpen))
	for i >= 0 {
		rest := bin[i+len(tagOpen):]
		if j := bytes.Index(rest, []byte(tagClose)); j > 0 && j < 40 {
			if t := string(rest[:j]); t != "dev" && parseVer(t) != nil {
				return t
			}
		}
		k := bytes.Index(rest, []byte(tagOpen))
		if k < 0 {
			break
		}
		i += len(tagOpen) + k
	}
	return ""
}

func SignedMessage(tag string, bin []byte) []byte {
	sum := sha256.Sum256(bin)
	return []byte(fmt.Sprintf("champ-select-companion|%s|%x", tag, sum))
}

// SignRelease returns the .sig file contents for an exe.
func SignRelease(priv ed25519.PrivateKey, bin []byte) (tag string, sig []byte, err error) {
	tag = TagInBinary(bin)
	if tag == "" {
		return "", nil, errors.New("this file isn't a Champ Select Companion release (no version inside)")
	}
	s := ed25519.Sign(priv, SignedMessage(tag, bin))
	return tag, []byte(base64.StdEncoding.EncodeToString(s) + "\n"), nil
}

// VerifyRelease checks an update before it is installed: signed by the owner's key, for this tag, and the tag is inside the exe.
func VerifyRelease(pubB64, tag string, bin, sigFile []byte) error {
	pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(pubB64))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("no valid update key is built into this version")
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigFile)))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("the update's signature file is damaged")
	}
	if got := TagInBinary(bin); got != tag {
		return fmt.Errorf("the update says it is %s but contains %q", tag, got)
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), SignedMessage(tag, bin), sig) {
		return errors.New("the update isn't signed with the owner's key")
	}
	return nil
}

// ---- the League client's certificate ----

// riotRoot is Riot's "LoL Game Engineering Certificate Authority" (riotgames.pem from github.com/RiotGames/leaguedirector,
// SHA-256 CA:8C:9D:32:5B:4C:DC:46:4C:6C:94:A5:85:C8:5E:91:EC:23:D4:0B:A5:BF:3A:E2:82:2B:95:1A:4A:50:4E:A3, valid to 2043).
// The client's local API presents a certificate signed by it.
const riotRoot = `-----BEGIN CERTIFICATE-----
MIIEIDCCAwgCCQDJC+QAdVx4UDANBgkqhkiG9w0BAQUFADCB0TELMAkGA1UEBhMC
VVMxEzARBgNVBAgTCkNhbGlmb3JuaWExFTATBgNVBAcTDFNhbnRhIE1vbmljYTET
MBEGA1UEChMKUmlvdCBHYW1lczEdMBsGA1UECxMUTG9MIEdhbWUgRW5naW5lZXJp
bmcxMzAxBgNVBAMTKkxvTCBHYW1lIEVuZ2luZWVyaW5nIENlcnRpZmljYXRlIEF1
dGhvcml0eTEtMCsGCSqGSIb3DQEJARYeZ2FtZXRlY2hub2xvZ2llc0ByaW90Z2Ft
ZXMuY29tMB4XDTEzMTIwNDAwNDgzOVoXDTQzMTEyNzAwNDgzOVowgdExCzAJBgNV
BAYTAlVTMRMwEQYDVQQIEwpDYWxpZm9ybmlhMRUwEwYDVQQHEwxTYW50YSBNb25p
Y2ExEzARBgNVBAoTClJpb3QgR2FtZXMxHTAbBgNVBAsTFExvTCBHYW1lIEVuZ2lu
ZWVyaW5nMTMwMQYDVQQDEypMb0wgR2FtZSBFbmdpbmVlcmluZyBDZXJ0aWZpY2F0
ZSBBdXRob3JpdHkxLTArBgkqhkiG9w0BCQEWHmdhbWV0ZWNobm9sb2dpZXNAcmlv
dGdhbWVzLmNvbTCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAKoJemF/
6PNG3GRJGbjzImTdOo1OJRDI7noRwJgDqkaJFkwv0X8aPUGbZSUzUO23cQcCgpYj
21ygzKu5dtCN2EcQVVpNtyPuM2V4eEGr1woodzALtufL3Nlyh6g5jKKuDIfeUBHv
JNyQf2h3Uha16lnrXmz9o9wsX/jf+jUAljBJqsMeACOpXfuZy+YKUCxSPOZaYTLC
y+0GQfiT431pJHBQlrXAUwzOmaJPQ7M6mLfsnpHibSkxUfMfHROaYCZ/sbWKl3lr
ZA9DbwaKKfS1Iw0ucAeDudyuqb4JntGU/W0aboKA0c3YB02mxAM4oDnqseuKV/CX
8SQAiaXnYotuNXMCAwEAATANBgkqhkiG9w0BAQUFAAOCAQEAf3KPmddqEqqC8iLs
lcd0euC4F5+USp9YsrZ3WuOzHqVxTtX3hR1scdlDXNvrsebQZUqwGdZGMS16ln3k
WObw7BbhU89tDNCN7Lt/IjT4MGRYRE+TmRc5EeIXxHkQ78bQqbmAI3GsW+7kJsoO
q3DdeE+M+BUJrhWorsAQCgUyZO166SAtKXKLIcxa+ddC49NvMQPJyzm3V+2b1roP
SvD2WV8gRYUnGmy/N0+u6ANq5EsbhZ548zZc+BI4upsWChTLyxt2RxR7+uGlS1+5
EcGfKZ+g024k/J32XP4hdho7WYAS2xMiV83CfLR/MNi8oSMaVQTdKD8cpgiWJk3L
XWehWA==
-----END CERTIFICATE-----`

// RiotTLS only accepts certificates signed by Riot's local root. The client's certificate doesn't name
// 127.0.0.1, so the host name isn't checked, but the signature is.
func RiotTLS() *tls.Config {
	root, _ := riotRootCert()
	return &tls.Config{
		InsecureSkipVerify: true, // replaced by the signature check below
		VerifyConnection: func(cs tls.ConnectionState) error {
			if root == nil || len(cs.PeerCertificates) == 0 {
				return errors.New("the League client sent no certificate")
			}
			if err := signedBy(cs.PeerCertificates, root); err != nil {
				return fmt.Errorf("not the League client (its certificate isn't Riot's): %w", err)
			}
			return nil
		},
	}
}

// signedBy checks that the first certificate is signed by root, directly or through the chain the server sent.
// Riot's local certificates date from 2013 and may use SHA-1, which Go's normal checks refuse, so RSA signatures
// with SHA-1 are checked by hand. This only proves the certificate came from Riot's local root.
func signedBy(chain []*x509.Certificate, root *x509.Certificate) error {
	for i := len(chain) - 1; i >= 0; i-- {
		parent := root
		if i+1 < len(chain) {
			parent = chain[i+1]
		}
		if err := checkSig(chain[i], parent); err != nil {
			if i == len(chain)-1 || checkSig(chain[i], root) != nil {
				return err
			}
		}
	}
	return nil
}

func checkSig(c, parent *x509.Certificate) error {
	if err := c.CheckSignatureFrom(parent); err == nil {
		return nil
	}
	pub, ok := parent.PublicKey.(*rsa.PublicKey)
	if !ok {
		return errors.New("unexpected key type")
	}
	var h crypto.Hash
	switch c.SignatureAlgorithm {
	case x509.SHA1WithRSA:
		h = crypto.SHA1
	case x509.SHA256WithRSA:
		h = crypto.SHA256
	case x509.SHA384WithRSA:
		h = crypto.SHA384
	case x509.SHA512WithRSA:
		h = crypto.SHA512
	default:
		return fmt.Errorf("unsupported signature %v", c.SignatureAlgorithm)
	}
	d := h.New()
	d.Write(c.RawTBSCertificate)
	return rsa.VerifyPKCS1v15(pub, h, d.Sum(nil), c.Signature)
}

func riotRootCert() (*x509.Certificate, error) {
	b, _ := pem.Decode([]byte(riotRoot))
	if b == nil {
		return nil, errors.New("bad built-in certificate")
	}
	return x509.ParseCertificate(b.Bytes)
}

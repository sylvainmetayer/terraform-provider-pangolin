package resources

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"
)

func testCAPEM(t *testing.T) (string, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test ca"},
		NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), der
}

func TestCertificateFingerprint(t *testing.T) {
	one, der := testCAPEM(t)
	two, _ := testCAPEM(t)

	sum := sha256.Sum256(der)
	var want []string
	for _, b := range sum {
		want = append(want, fmt.Sprintf("%02X", b))
	}
	got, err := certificateFingerprint("# comment\n" + one)
	if err != nil || got != strings.Join(want, ":") {
		t.Fatalf("got %q, %v; want %q", got, err, strings.Join(want, ":"))
	}

	for name, in := range map[string]string{
		"empty":   "",
		"bundle":  one + two,
		"key":     "-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n",
		"garbage": "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n",
	} {
		if _, err := certificateFingerprint(in); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

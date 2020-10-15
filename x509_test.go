// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"io"
	"io/ioutil"
	logpkg "log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type keygen func(t *testing.T) (pub, priv interface{}, name string)

func ed25519Keygen() keygen {
	return func(t *testing.T) (pub, priv interface{}, name string) {
		seed := make([]byte, ed25519.SeedSize)
		_, err := io.ReadFull(rand.Reader, seed)
		if err != nil {
			t.Fatal(err)
		}
		key := ed25519.NewKeyFromSeed(seed)
		return key.Public(), key, "ed25519"
	}
}

func ecKeygen(curve elliptic.Curve) keygen {
	return func(t *testing.T) (pub, priv interface{}, name string) {
		var key *ecdsa.PrivateKey
		key, err := ecdsa.GenerateKey(curve, rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return key.Public(), key, curve.Params().Name
	}
}

func TestClientCert(t *testing.T) {
	algos := []keygen{
		ed25519Keygen(),
		ecKeygen(elliptic.P256()),
		ecKeygen(elliptic.P384()),
		ecKeygen(elliptic.P521()),
	}

	for _, algo := range algos {
		pub, priv, name := algo(t)
		testClientCert(t, pub, priv, name)
	}
}

func echo(w http.ResponseWriter, r *http.Request) {
	io.Copy(w, r.Body)
}

func testClientCert(t *testing.T, pub, priv interface{}, name string) {
	ca, err := generateAuthority(pub, priv)
	if err != nil {
		t.Error(err)
		return
	}
	keyBlock, err := marshalPrivateKey(ca.PrivateKey)
	if err != nil {
		t.Error(err)
		return
	}
	certBlock, err := createSignedClientCert(pub, ca.PrivateKey, ca.Cert)
	if err != nil {
		t.Error(err)
		return
	}
	keypair, err := tls.X509KeyPair(certBlock, keyBlock)
	if err != nil {
		t.Error(err)
		return
	}

	s := httptest.NewUnstartedServer(http.HandlerFunc(echo))
	s.TLS = &tls.Config{
		MinVersion: tls.VersionTLS12,
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  x509.NewCertPool(),
	}
	s.TLS.ClientCAs.AddCert(ca.Cert)
	defer s.Close()
	s.StartTLS()

	client := s.Client()
	tr := client.Transport.(*http.Transport)
	tr.TLSClientConfig.Certificates = []tls.Certificate{keypair}

	req, err := http.NewRequest("PUT", s.URL, strings.NewReader("balls"))
	if err != nil {
		t.Error(err)
		return
	}
	resp, err := s.Client().Do(req)
	if err != nil {
		t.Errorf("algorithm %s: %v", name, err)
		return
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Error(err)
		return
	}
	if !bytes.Equal(body, []byte("balls")) {
		t.Errorf("echo handler did not return expected result")
	}
}

func TestUntrustedClientCert(t *testing.T) {
	algo := ed25519Keygen()
	pub1, priv1, _ := algo(t) // trusted by server
	pub2, priv2, _ := algo(t) // presented by client

	ca1, err := generateAuthority(pub1, priv1)
	if err != nil {
		t.Error(err)
		return
	}

	ca2, err := generateAuthority(pub2, priv2)
	if err != nil {
		t.Error(err)
		return
	}
	keyBlock2, err := marshalPrivateKey(ca2.PrivateKey)
	if err != nil {
		t.Error(err)
		return
	}
	certBlock2, err := createSignedClientCert(pub2, ca2.PrivateKey, ca2.Cert)
	if err != nil {
		t.Error(err)
		return
	}
	keypair2, err := tls.X509KeyPair(certBlock2, keyBlock2)
	if err != nil {
		t.Error(err)
		return
	}

	s := httptest.NewUnstartedServer(http.HandlerFunc(echo))
	s.Config = &http.Server{
		// Don't log remote cert errors for this negative test
		ErrorLog: logpkg.New(ioutil.Discard, "", 0),
	}
	s.TLS = &tls.Config{
		MinVersion: tls.VersionTLS12,
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  x509.NewCertPool(),
	}
	s.TLS.ClientCAs.AddCert(ca1.Cert)
	defer s.Close()
	s.StartTLS()

	client := s.Client()
	tr := client.Transport.(*http.Transport)
	tr.TLSClientConfig.Certificates = []tls.Certificate{keypair2}

	req, err := http.NewRequest("PUT", s.URL, strings.NewReader("balls"))
	if err != nil {
		t.Error(err)
		return
	}
	_, err = s.Client().Do(req)
	if err == nil {
		t.Errorf("request with bad client cert did not error")
		return
	}
	if !strings.HasSuffix(err.Error(), "tls: bad certificate") {
		t.Errorf("server did not report bad certificate error; "+
			"instead errored with: %v", err)
		return
	}
}

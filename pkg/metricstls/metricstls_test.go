package metricstls

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestAllowPrometheusK8s(t *testing.T) {
	handler := AllowPrometheusK8s(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	tests := []struct {
		name string
		tls  *tls.ConnectionState
		want int
	}{
		{name: "no client certificate", want: http.StatusUnauthorized},
		{name: "other client certificate", tls: &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{Subject: pkix.Name{CommonName: "other"}}}}, want: http.StatusForbidden},
		{name: "prometheus client certificate", tls: &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{Subject: pkix.Name{CommonName: PrometheusK8sCN}}}}, want: http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			req.TLS = tt.tls
			resp := httptest.NewRecorder()
			handler.ServeHTTP(resp, req)
			if resp.Code != tt.want {
				t.Fatalf("status = %d, want %d", resp.Code, tt.want)
			}
		})
	}
}

func TestTLSProfileInputs(t *testing.T) {
	version, err := parseMinVersion("VersionTLS13")
	if err != nil || version != tls.VersionTLS13 {
		t.Fatalf("VersionTLS13 = %#x, %v", version, err)
	}
	if _, err := parseMinVersion("not-a-version"); err == nil {
		t.Fatal("invalid TLS version was accepted")
	}

	if _, err := parseMinVersion("TLS1.3"); err == nil {
		t.Fatal("non-OpenShift TLS version was accepted")
	}

	// These are the OpenSSL names produced by the OpenShift TLS profile API.
	ciphers, err := parseCipherSuites("ECDHE-ECDSA-AES128-GCM-SHA256,ECDHE-RSA-CHACHA20-POLY1305")
	if err != nil || len(ciphers) != 2 {
		t.Fatalf("OpenSSL cipher input = %v, %v", ciphers, err)
	}
	// The full Old profile is delegated to library-go, exactly as Autopilot's
	// TLS resolver does.
	ciphers, err = parseCipherSuites("TLS_AES_128_GCM_SHA256,TLS_AES_256_GCM_SHA384,TLS_CHACHA20_POLY1305_SHA256,ECDHE-ECDSA-AES128-GCM-SHA256,ECDHE-RSA-AES128-GCM-SHA256,ECDHE-ECDSA-AES256-GCM-SHA384,ECDHE-RSA-AES256-GCM-SHA384,ECDHE-ECDSA-CHACHA20-POLY1305,ECDHE-RSA-CHACHA20-POLY1305,ECDHE-ECDSA-AES128-SHA256,ECDHE-RSA-AES128-SHA256,ECDHE-ECDSA-AES128-SHA,ECDHE-RSA-AES128-SHA,ECDHE-ECDSA-AES256-SHA,ECDHE-RSA-AES256-SHA,AES128-GCM-SHA256,AES256-GCM-SHA384,AES128-SHA256,AES128-SHA,AES256-SHA,DES-CBC3-SHA")
	if err != nil || len(ciphers) != 21 {
		t.Fatalf("Old profile cipher input = %v, %v", ciphers, err)
	}
	if _, err := parseCipherSuites("not-a-cipher"); err == nil {
		t.Fatal("invalid cipher was accepted")
	}
	if _, err := parseCipherSuites("ECDHE-ECDSA-AES128-GCM-SHA256,not-a-cipher"); err == nil {
		t.Fatal("a cipher list containing an invalid cipher was accepted")
	}
}

func TestTLSGroups(t *testing.T) {
	for _, tc := range []struct {
		name   string
		groups string
		want   []tls.CurveID
	}{
		{
			name: "empty group list", groups: "", want: nil,
		},
		{
			name: "one group in the list", groups: "X25519", want: []tls.CurveID{tls.X25519},
		},
		{
			name: "multiple groups in the list", groups: "X25519,secp256r1,secp384r1", want: []tls.CurveID{tls.X25519, tls.CurveP256, tls.CurveP384},
		},
		{
			name: "multiple groups in the list with spaces", groups: " X25519 , secp256r1 , secp384r1 ", want: []tls.CurveID{tls.X25519, tls.CurveP256, tls.CurveP384},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := parseTLSGroups(tc.groups)
			if err != nil {
				t.Fatalf("parseTLSGroups(%q): returned error: %v", tc.groups, err)
			}

			if !slices.Equal(res, tc.want) {
				t.Errorf("parseTLSGroups(%q) = %v, want %v", tc.groups, res, tc.want)
			}
		})
	}
}

func TestTLSGroups_unknownGroup(t *testing.T) {
	for _, tc := range []struct {
		name   string
		groups string
	}{
		{
			name: "single unknow group", groups: "unknownGroup",
		},
		{
			name: "mix valid and non-valid groups", groups: "X25519,unknownGroup,secp256r1,secp384r1",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseTLSGroups(tc.groups)
			if err == nil {
				t.Errorf("should return error, but didn't")
			}
		})
	}
}

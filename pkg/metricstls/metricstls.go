// Package metricstls provides mTLS configuration for the OpenShift metrics endpoint.
package metricstls

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"

	"github.com/openshift/library-go/pkg/crypto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

const (
	ClientCAConfigMapNamespace = "kube-system"
	ClientCAConfigMapName      = "extension-apiserver-authentication"
	ClientCAConfigMapKey       = "client-ca-file"
	PrometheusK8sCN            = "system:serviceaccount:openshift-monitoring:prometheus-k8s"
)

type ClientCAPool struct{ pool atomic.Pointer[x509.CertPool] }

func (p *ClientCAPool) Set(caPEM []byte) error {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return fmt.Errorf("no valid certificates found in client CA bundle")
	}
	p.pool.Store(pool)
	return nil
}
func (p *ClientCAPool) Get() *x509.CertPool { return p.pool.Load() }

// LoadClientCAFile loads a caller-managed PEM bundle. This permits an
// orchestrator such as virt-platform-autopilot to supply its resolved trust
// source without granting the exporter access to cluster-wide ConfigMaps.
func LoadClientCAFile(path string) (*ClientCAPool, error) {
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read metrics client CA: %w", err)
	}
	pool := &ClientCAPool{}
	if err := pool.Set(pem); err != nil {
		return nil, err
	}
	return pool, nil
}

// StartClientCAWatcher loads the authoritative OpenShift client CA and watches it for rotation.
func StartClientCAWatcher(ctx context.Context, client kubernetes.Interface) (*ClientCAPool, error) {
	cm, err := client.CoreV1().ConfigMaps(ClientCAConfigMapNamespace).Get(ctx, ClientCAConfigMapName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get metrics client CA: %w", err)
	}
	pool := &ClientCAPool{}
	if err := setFromData(pool, cm.Data); err != nil {
		return nil, err
	}
	informer := corev1informers.NewFilteredConfigMapInformer(
		client,
		ClientCAConfigMapNamespace,
		0,
		cache.Indexers{},
		func(options *metav1.ListOptions) {
			options.FieldSelector = fields.OneTermEqualSelector("metadata.name", ClientCAConfigMapName).String()
		},
	)
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{AddFunc: func(obj interface{}) { updatePool(pool, obj) }, UpdateFunc: func(_, obj interface{}) { updatePool(pool, obj) }})
	go informer.Run(ctx.Done())
	return pool, nil
}
func setFromData(pool *ClientCAPool, data map[string]string) error {
	ca := data[ClientCAConfigMapKey]
	if ca == "" {
		return fmt.Errorf("metrics client CA ConfigMap missing key %q", ClientCAConfigMapKey)
	}
	return pool.Set([]byte(ca))
}
func updatePool(pool *ClientCAPool, obj interface{}) {
	cm, ok := obj.(*corev1.ConfigMap)
	if ok && cm.Name == ClientCAConfigMapName {
		_ = setFromData(pool, cm.Data)
	}
}

// ServerConfig requires a verified client certificate and reloads the service-ca cert per connection.
// minVersion and cipherSuites are intentionally supplied by the deployer: this
// lets virt-platform-autopilot resolve the HCO/APIServer TLS profile and pass
// its effective policy to KME without duplicating that policy logic here.
func ServerConfig(certFile, keyFile string, pool *ClientCAPool, minVersion, cipherSuites string) (*tls.Config, error) {
	minimum, err := parseMinVersion(minVersion)
	if err != nil {
		return nil, err
	}
	ciphers, err := parseCipherSuites(cipherSuites)
	if err != nil {
		return nil, err
	}
	config := &tls.Config{MinVersion: minimum, CipherSuites: ciphers}
	config.GetCertificate = func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("load serving certificate: %w", err)
		}
		return &cert, nil
	}
	config.GetConfigForClient = func(*tls.ClientHelloInfo) (*tls.Config, error) {
		ca := pool.Get()
		if ca == nil {
			return nil, fmt.Errorf("metrics client CA is not available")
		}
		c := config.Clone()
		c.GetConfigForClient = nil
		c.ClientAuth = tls.RequireAndVerifyClientCert
		c.ClientCAs = ca
		return c, nil
	}
	return config, nil
}

func parseMinVersion(value string) (uint16, error) {
	return crypto.TLSVersion(strings.TrimSpace(value))
}

func parseCipherSuites(value string) ([]uint16, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	// OpenShift TLS profiles use OpenSSL names. library-go owns the canonical
	// conversion and cipher registry, keeping this endpoint in lockstep with
	// the platform profile resolver.
	result := make([]uint16, 0)
	for _, configuredName := range strings.Split(value, ",") {
		configuredName = strings.TrimSpace(configuredName)
		if configuredName == "" {
			continue
		}
		ianaNames := crypto.OpenSSLToIANACipherSuites([]string{configuredName})
		if len(ianaNames) != 1 {
			return nil, fmt.Errorf("unsupported OpenShift TLS cipher suite %q", configuredName)
		}
		name := ianaNames[0]
		suite, err := crypto.CipherSuite(name)
		if err != nil {
			return nil, fmt.Errorf("resolve TLS cipher suite %q: %w", name, err)
		}
		result = append(result, suite)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("TLS cipher suites must contain at least one suite")
	}
	return result, nil
}

// AllowPrometheusK8s authorizes only the authenticated in-cluster Prometheus identity.
func AllowPrometheusK8s(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			http.Error(w, "client certificate required", http.StatusUnauthorized)
			return
		}
		if r.TLS.PeerCertificates[0].Subject.CommonName != PrometheusK8sCN {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

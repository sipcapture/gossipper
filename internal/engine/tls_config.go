package engine

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

func (e *Engine) clientTLSConfig() (*tls.Config, error) {
	cfg := &tls.Config{
		InsecureSkipVerify: e.cfg.TLSSkipVerify,
		MinVersion:         tls.VersionTLS12,
	}
	if e.cfg.TLSCAFile != "" {
		pool := x509.NewCertPool()
		data, err := os.ReadFile(e.cfg.TLSCAFile)
		if err != nil {
			return nil, err
		}
		if !pool.AppendCertsFromPEM(data) {
			return nil, fmt.Errorf("failed to parse TLS CA file %q", e.cfg.TLSCAFile)
		}
		cfg.RootCAs = pool
		if e.cfg.TLSSkipVerify == false {
			cfg.InsecureSkipVerify = false
		}
	}
	if e.cfg.TLSCertFile != "" || e.cfg.TLSKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(e.cfg.TLSCertFile, e.cfg.TLSKeyFile)
		if err != nil {
			return nil, err
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}

func (e *Engine) serverTLSConfig() (*tls.Config, error) {
	if e.cfg.TLSCertFile == "" || e.cfg.TLSKeyFile == "" {
		return nil, fmt.Errorf("TLS server mode requires tls_cert and tls_key")
	}
	cert, err := tls.LoadX509KeyPair(e.cfg.TLSCertFile, e.cfg.TLSKeyFile)
	if err != nil {
		return nil, err
	}
	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
	if e.cfg.TLSCAFile != "" {
		pool := x509.NewCertPool()
		data, err := os.ReadFile(e.cfg.TLSCAFile)
		if err != nil {
			return nil, err
		}
		if !pool.AppendCertsFromPEM(data) {
			return nil, fmt.Errorf("failed to parse TLS CA file %q", e.cfg.TLSCAFile)
		}
		cfg.ClientCAs = pool
	}
	return cfg, nil
}

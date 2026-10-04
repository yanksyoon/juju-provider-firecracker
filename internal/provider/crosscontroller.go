package provider

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"strings"
)

// CrossControllerConfig is the non-secret contract for an opt-in Juju
// cross-controller smoke test. It deliberately contains no credentials,
// kubeconfig, macaroon, or token. Controller registration, model selection,
// and offer/consume are separate operations in the smoke procedure.
type CrossControllerConfig struct {
	Enabled          bool     `json:"enabled"`
	ControllerName   string   `json:"controller_name,omitempty"`
	APIAddresses     []string `json:"api_addresses,omitempty"`
	CACertificatePEM string   `json:"ca_certificate_pem,omitempty"`
	ModelName        string   `json:"model_name,omitempty"`
	ModelUUID        string   `json:"model_uuid,omitempty"`
	OfferName        string   `json:"offer_name,omitempty"`
	Cleanup          bool     `json:"cleanup"`
}

// ParseCrossControllerConfig decodes a strict JSON contract and validates it.
// Disabled configurations are accepted without requiring any endpoint data.
func ParseCrossControllerConfig(data []byte) (CrossControllerConfig, error) {
	var cfg CrossControllerConfig
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return CrossControllerConfig{}, fmt.Errorf("cross-controller settings: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return CrossControllerConfig{}, fmt.Errorf("cross-controller settings: trailing data")
	}
	if err := cfg.Validate(); err != nil {
		return CrossControllerConfig{}, err
	}
	return cfg, nil
}

func (c CrossControllerConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.ControllerName) == "" {
		return fmt.Errorf("cross-controller controller_name is required when enabled")
	}
	if len(c.APIAddresses) == 0 {
		return fmt.Errorf("cross-controller api_addresses are required when enabled")
	}
	for _, address := range c.APIAddresses {
		host, port, err := net.SplitHostPort(address)
		if err != nil || host == "" || port == "" {
			return fmt.Errorf("cross-controller api address must be host:port: %q", address)
		}
	}
	if c.CACertificatePEM != "" {
		pool := x509.NewCertPool()
		if ok := pool.AppendCertsFromPEM([]byte(c.CACertificatePEM)); !ok {
			return fmt.Errorf("cross-controller ca_certificate_pem is not a valid certificate")
		}
		remaining := []byte(c.CACertificatePEM)
		isCA := false
		for {
			block, rest := pem.Decode(remaining)
			if block == nil {
				break
			}
			remaining = rest
			if block.Type != "CERTIFICATE" {
				continue
			}
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return fmt.Errorf("cross-controller ca_certificate_pem is not a valid certificate: %w", err)
			}
			isCA = isCA || cert.IsCA
		}
		if !isCA {
			return fmt.Errorf("cross-controller ca_certificate_pem must contain a CA certificate")
		}
	}
	if strings.TrimSpace(c.ModelName) == "" && strings.TrimSpace(c.ModelUUID) == "" {
		return fmt.Errorf("cross-controller model_name or model_uuid is required when enabled")
	}
	if strings.TrimSpace(c.OfferName) == "" {
		return fmt.Errorf("cross-controller offer_name is required when enabled")
	}
	return nil
}

// MarshalCrossControllerConfig provides the stable, secret-free wire format.
func MarshalCrossControllerConfig(cfg CrossControllerConfig) ([]byte, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(cfg)
}

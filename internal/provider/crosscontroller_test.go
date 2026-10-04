package provider

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCrossControllerConfigRoundTrip(t *testing.T) {
	original := CrossControllerConfig{
		Enabled:        true,
		ControllerName: "disposable-k8s",
		APIAddresses:   []string{"127.0.0.1:17070"},
		ModelName:      "fc-compat",
		OfferName:      "fc-offer",
		Cleanup:        true,
	}
	data, err := MarshalCrossControllerConfig(original)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := ParseCrossControllerConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ControllerName != original.ControllerName || decoded.ModelName != original.ModelName || decoded.OfferName != original.OfferName || len(decoded.APIAddresses) != 1 || !decoded.Cleanup {
		t.Fatalf("round-trip changed settings: %#v", decoded)
	}

	if strings.Contains(string(data), "token") || strings.Contains(string(data), "password") || strings.Contains(string(data), "kubeconfig") {
		t.Fatalf("wire format contains a forbidden secret field: %s", data)
	}
}

func TestCrossControllerConfigDisabledNeedsNoSecrets(t *testing.T) {
	cfg, err := ParseCrossControllerConfig([]byte(`{"enabled":false,"cleanup":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Enabled || !cfg.Cleanup {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

func TestCrossControllerConfigFailsClosed(t *testing.T) {
	tests := []string{
		`{"enabled":true}`,
		`{"enabled":true,"controller_name":"k8s","api_addresses":["127.0.0.1"],"model_name":"m","offer_name":"o"}`,
		`{"enabled":true,"controller_name":"k8s","api_addresses":["127.0.0.1:17070"],"model_name":"m","offer_name":"o","unexpected":"x"}`,
		`{"enabled":true,"controller_name":"k8s","api_addresses":["127.0.0.1:17070"],"model_name":"m","offer_name":"o"} trailing`,
	}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			if _, err := ParseCrossControllerConfig([]byte(input)); err == nil {
				t.Fatal("expected malformed or incomplete settings to fail closed")
			}
		})
	}
}

func TestCrossControllerConfigRejectsInvalidCA(t *testing.T) {
	cfg := CrossControllerConfig{Enabled: true, ControllerName: "k8s", APIAddresses: []string{"127.0.0.1:17070"}, ModelUUID: "uuid", OfferName: "offer", CACertificatePEM: "not-a-certificate"}
	if _, err := MarshalCrossControllerConfig(cfg); err == nil {
		t.Fatal("expected invalid CA to fail")
	}
}

func TestCrossControllerConfigJSONIsStable(t *testing.T) {
	cfg := CrossControllerConfig{Enabled: false}
	data, err := json.Marshal(cfg)
	if err != nil || string(data) != `{"enabled":false,"cleanup":false}` {
		t.Fatalf("unexpected JSON: %s (%v)", data, err)
	}
}

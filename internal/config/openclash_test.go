package config

import "testing"

func openClashProvider(baseURL string) ProviderConfig {
	return ProviderConfig{
		ID: "clash", Type: "openclash", AccountLabel: "router", Region: "custom",
		BaseURL: baseURL, Interval: "5s", StaleAfter: "15s", SecretRef: "keyring:openclash",
	}
}

func validateOpenClashProvider(providers ...ProviderConfig) error {
	cfg := Config{
		SchemaVersion: SchemaVersion, SourceNode: SourceNodeConfig{ID: "node"},
		Listen: ListenConfig{Addr: "127.0.0.1:8443"}, Providers: providers,
	}
	return cfg.Validate()
}

func TestOpenClashConfigAllowsOnlySecureOriginsOrLiteralPrivateHTTP(t *testing.T) {
	valid := []string{
		"http://127.0.0.1:9090", "http://192.168.31.1:9090/", "http://[::1]:9090",
		"http://[fd00::1]:9090", "https://router.example.com", "https://8.8.8.8:9090",
	}
	for _, baseURL := range valid {
		if err := validateOpenClashProvider(openClashProvider(baseURL)); err != nil {
			t.Errorf("valid OpenClash URL %q rejected: %v", baseURL, err)
		}
	}
	invalid := []string{
		"http://localhost:9090", "http://router.lan:9090", "http://8.8.8.8:9090",
		"https://router.example.com/api", "https://router.example.com?token=x",
		"https://user@router.example.com", "https://169.254.169.254",
	}
	for _, baseURL := range invalid {
		if err := validateOpenClashProvider(openClashProvider(baseURL)); err == nil {
			t.Errorf("invalid OpenClash URL %q accepted", baseURL)
		}
	}
}

func TestOpenClashConfigRequiresSecret(t *testing.T) {
	provider := openClashProvider("http://127.0.0.1:9090")
	provider.SecretRef = ""
	if err := validateOpenClashProvider(provider); err == nil {
		t.Fatal("OpenClash without secret_ref accepted")
	}
}

func TestOpenClashConfigAllowsOnlyOneEnabledProvider(t *testing.T) {
	first := openClashProvider("http://127.0.0.1:9090")
	second := openClashProvider("http://192.168.31.2:9090")
	second.ID = "clash-second"
	if err := validateOpenClashProvider(first, second); err == nil {
		t.Fatal("two enabled OpenClash providers accepted")
	}

	disabled := false
	second.Enabled = &disabled
	if err := validateOpenClashProvider(first, second); err != nil {
		t.Fatalf("disabled second OpenClash provider rejected: %v", err)
	}
}

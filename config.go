package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sweb "github.com/sanchpet/sweb-go-sdk"
)

// defaultAPIURL is the production SpaceWeb API root, used when the Issuer config
// omits apiUrl.
const defaultAPIURL = "https://api.sweb.ru"

// spacewebConfig is the per-Issuer webhook.config block (dns01.webhook.config).
// Credentials are never inlined here: secretRef points at a Secret in the
// challenge's ResourceNamespace holding "login" and "password".
type spacewebConfig struct {
	// APIURL overrides the SpaceWeb API root; defaults to defaultAPIURL.
	APIURL string `json:"apiUrl"`

	// SecretRef names the Secret holding the control-panel credentials.
	SecretRef struct {
		Name string `json:"name"`
	} `json:"secretRef"`
}

// loadConfig decodes the Issuer's webhook config JSON into a spacewebConfig,
// applying the apiUrl default.
func loadConfig(cfgJSON *apiextensionsv1.JSON) (spacewebConfig, error) {
	cfg := spacewebConfig{}
	if cfgJSON != nil {
		if err := json.Unmarshal(cfgJSON.Raw, &cfg); err != nil {
			return cfg, fmt.Errorf("decoding solver config: %w", err)
		}
	}
	if cfg.APIURL == "" {
		cfg.APIURL = defaultAPIURL
	}
	return cfg, nil
}

// newSWEBClient reads the referenced Secret from the challenge's
// ResourceNamespace and builds a SpaceWeb client authenticated with the
// control-panel login+password. WithCredentials (not a static token) lets the
// SDK transparently re-exchange them when the short-lived session token expires.
func (s *spacewebSolver) newSWEBClient(ctx context.Context, ch *v1alpha1.ChallengeRequest, cfg spacewebConfig) (*sweb.Client, error) {
	if s.client == nil {
		return nil, errors.New("solver not initialized: kubernetes clientset is nil")
	}
	if cfg.SecretRef.Name == "" {
		return nil, errors.New("solver config: secretRef.name must be set")
	}

	secret, err := s.client.CoreV1().Secrets(ch.ResourceNamespace).Get(ctx, cfg.SecretRef.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting secret %s/%s: %w", ch.ResourceNamespace, cfg.SecretRef.Name, err)
	}

	login, err := secretValue(secret, "login")
	if err != nil {
		return nil, err
	}
	password, err := secretValue(secret, "password")
	if err != nil {
		return nil, err
	}

	opts := []sweb.Option{sweb.WithCredentials(login, password)}
	if cfg.APIURL != "" {
		opts = append(opts, sweb.WithBaseURL(cfg.APIURL))
	}
	return sweb.New(opts...), nil
}

// secretValue extracts a required key from a Secret, erroring if it is absent or
// empty.
func secretValue(secret *corev1.Secret, key string) (string, error) {
	v, ok := secret.Data[key]
	if !ok || len(v) == 0 {
		return "", fmt.Errorf("secret %s/%s missing key %q", secret.Namespace, secret.Name, key)
	}
	return string(v), nil
}

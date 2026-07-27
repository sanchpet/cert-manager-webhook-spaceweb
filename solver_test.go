package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/sanchpet/sweb-go-sdk/dns"
	"github.com/sanchpet/sweb-go-sdk/flex"
)

// mockSWEB is an in-memory SpaceWeb JSON-RPC server for the /domains/dns
// endpoint. It emulates just the methods the solver uses — getToken, info, and
// editTxt (add/del) — over a mutable zone, and counts adds/dels so tests can
// assert idempotency and match-by-value deletion. It is safe for concurrent use.
type mockSWEB struct {
	mu       sync.Mutex
	records  []dns.Record
	addCount int
	delCount int
}

// newMockSWEB seeds the zone with any pre-existing records.
func newMockSWEB(seed ...dns.Record) *mockSWEB {
	return &mockSWEB{records: append([]dns.Record{}, seed...)}
}

func (m *mockSWEB) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(m.handle))
	t.Cleanup(srv.Close)
	return srv
}

func (m *mockSWEB) handle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	switch req.Method {
	case "getToken":
		writeResult(w, json.RawMessage(`"test-token"`))
	case "info":
		m.mu.Lock()
		recs := append([]dns.Record{}, m.records...)
		m.mu.Unlock()
		b, _ := json.Marshal(recs)
		writeResult(w, b)
	case "editTxt":
		var p struct {
			Action    string `json:"action"`
			Index     int    `json:"index"`
			SubDomain string `json:"subDomain"`
			Value     string `json:"value"`
		}
		_ = json.Unmarshal(req.Params, &p)
		m.editTxt(p.Action, p.Index, p.SubDomain, p.Value)
		writeResult(w, json.RawMessage(`true`))
	default:
		http.Error(w, "unexpected method "+req.Method, http.StatusBadRequest)
	}
}

func (m *mockSWEB) editTxt(action string, index int, subDomain, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch action {
	case "add":
		m.addCount++
		domain := subDomain
		if domain == "" {
			domain = "@" // the API reports the apex as "@"
		}
		// Index is per-category positional; assign the next TXT slot.
		m.records = append(m.records, dns.Record{
			Index:    flex.Int(m.txtCountLocked()),
			Type:     "TXT",
			Category: "mainTxt",
			Domain:   domain,
			Value:    value,
		})
	case "del":
		m.delCount++
		for i, r := range m.records {
			if r.Type == "TXT" && int(r.Index) == index {
				m.records = append(m.records[:i], m.records[i+1:]...)
				return
			}
		}
	}
}

func (m *mockSWEB) txtCountLocked() int {
	n := 0
	for _, r := range m.records {
		if r.Type == "TXT" {
			n++
		}
	}
	return n
}

func (m *mockSWEB) counts() (add, del int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.addCount, m.delCount
}

func (m *mockSWEB) txtValues() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, r := range m.records {
		if r.Type == "TXT" {
			out = append(out, r.Value)
		}
	}
	return out
}

func writeResult(w http.ResponseWriter, result json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":`))
	_, _ = w.Write(result)
	_, _ = w.Write([]byte(`}`))
}

// testSolver builds a solver backed by a fake clientset holding the credential
// Secret the config references.
func testSolver(t *testing.T) *spacewebSolver {
	t.Helper()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "spaceweb-credentials", Namespace: "default"},
		Data:       map[string][]byte{"login": []byte("user"), "password": []byte("pass")},
	}
	return &spacewebSolver{client: fake.NewClientset(secret)}
}

// challenge builds a ChallengeRequest pointing the solver at the mock server.
func challenge(t *testing.T, apiURL, key string) *v1alpha1.ChallengeRequest {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"apiUrl":    apiURL,
		"secretRef": map[string]string{"name": "spaceweb-credentials"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &v1alpha1.ChallengeRequest{
		ResolvedFQDN:      "_acme-challenge.example.com.",
		ResolvedZone:      "example.com.",
		ResourceNamespace: "default",
		Key:               key,
		Config:            &apiextensionsv1.JSON{Raw: raw},
	}
}

func TestSplitFQDN(t *testing.T) {
	tests := []struct {
		name          string
		fqdn, zone    string
		wantDomain    string
		wantSubDomain string
	}{
		{"acme challenge", "_acme-challenge.example.com.", "example.com.", "example.com", "_acme-challenge"},
		{"wildcard maps to same subdomain", "_acme-challenge.example.com.", "example.com.", "example.com", "_acme-challenge"},
		{"nested subdomain", "_acme-challenge.foo.example.com.", "example.com.", "example.com", "_acme-challenge.foo"},
		{"subzone", "_acme-challenge.foo.example.com.", "foo.example.com.", "foo.example.com", "_acme-challenge"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ch := &v1alpha1.ChallengeRequest{ResolvedFQDN: tc.fqdn, ResolvedZone: tc.zone}
			domain, sub := splitFQDN(ch)
			if domain != tc.wantDomain || sub != tc.wantSubDomain {
				t.Errorf("splitFQDN(%q,%q) = (%q,%q), want (%q,%q)",
					tc.fqdn, tc.zone, domain, sub, tc.wantDomain, tc.wantSubDomain)
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	t.Run("nil config defaults apiUrl", func(t *testing.T) {
		cfg, err := loadConfig(nil)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.APIURL != defaultAPIURL {
			t.Errorf("ApiUrl = %q, want default %q", cfg.APIURL, defaultAPIURL)
		}
	})
	t.Run("empty apiUrl defaults", func(t *testing.T) {
		cfg, err := loadConfig(&apiextensionsv1.JSON{Raw: []byte(`{"secretRef":{"name":"s"}}`)})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.APIURL != defaultAPIURL {
			t.Errorf("ApiUrl = %q, want default", cfg.APIURL)
		}
		if cfg.SecretRef.Name != "s" {
			t.Errorf("SecretRef.Name = %q, want s", cfg.SecretRef.Name)
		}
	})
	t.Run("explicit apiUrl honoured", func(t *testing.T) {
		cfg, err := loadConfig(&apiextensionsv1.JSON{Raw: []byte(`{"apiUrl":"https://staging.example","secretRef":{"name":"s"}}`)})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.APIURL != "https://staging.example" {
			t.Errorf("ApiUrl = %q, want staging", cfg.APIURL)
		}
	})
	t.Run("malformed json errors", func(t *testing.T) {
		if _, err := loadConfig(&apiextensionsv1.JSON{Raw: []byte(`{bad`)}); err == nil {
			t.Error("expected error for malformed config")
		}
	})
}

func TestPresentAddsRecord(t *testing.T) {
	mock := newMockSWEB()
	srv := mock.server(t)
	s := testSolver(t)

	if err := s.Present(challenge(t, srv.URL, "key-abc")); err != nil {
		t.Fatalf("Present: %v", err)
	}
	if add, _ := mock.counts(); add != 1 {
		t.Errorf("addCount = %d, want 1", add)
	}
	if got := mock.txtValues(); len(got) != 1 || got[0] != "key-abc" {
		t.Errorf("zone TXT values = %v, want [key-abc]", got)
	}
}

// TestPresentIdempotent asserts that presenting the same (name,value) twice does
// not add a second record — SpaceWeb's add is not idempotent, so the solver must
// list-then-skip.
func TestPresentIdempotent(t *testing.T) {
	mock := newMockSWEB()
	srv := mock.server(t)
	s := testSolver(t)
	ch := challenge(t, srv.URL, "key-abc")

	if err := s.Present(ch); err != nil {
		t.Fatalf("first Present: %v", err)
	}
	if err := s.Present(ch); err != nil {
		t.Fatalf("second Present: %v", err)
	}
	if add, _ := mock.counts(); add != 1 {
		t.Errorf("addCount = %d, want 1 (second Present must no-op)", add)
	}
}

// TestPresentBaseAndWildcard covers a certificate with a base SAN and a wildcard
// SAN: two challenges on the same _acme-challenge name but different keys. Both
// records must be added (different values ⇒ not a no-op).
func TestPresentBaseAndWildcard(t *testing.T) {
	mock := newMockSWEB()
	srv := mock.server(t)
	s := testSolver(t)

	if err := s.Present(challenge(t, srv.URL, "key-base")); err != nil {
		t.Fatalf("Present base: %v", err)
	}
	if err := s.Present(challenge(t, srv.URL, "key-wildcard")); err != nil {
		t.Fatalf("Present wildcard: %v", err)
	}
	if add, _ := mock.counts(); add != 2 {
		t.Errorf("addCount = %d, want 2", add)
	}
	if got := len(mock.txtValues()); got != 2 {
		t.Errorf("zone TXT count = %d, want 2", got)
	}
}

// TestCleanUpMatchByValue asserts CleanUp removes the TXT matching ch.Key and
// leaves a co-located record with a different value intact.
func TestCleanUpMatchByValue(t *testing.T) {
	mock := newMockSWEB()
	srv := mock.server(t)
	s := testSolver(t)

	if err := s.Present(challenge(t, srv.URL, "key-base")); err != nil {
		t.Fatalf("Present base: %v", err)
	}
	if err := s.Present(challenge(t, srv.URL, "key-wildcard")); err != nil {
		t.Fatalf("Present wildcard: %v", err)
	}

	if err := s.CleanUp(challenge(t, srv.URL, "key-base")); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	if _, del := mock.counts(); del != 1 {
		t.Errorf("delCount = %d, want 1", del)
	}
	got := mock.txtValues()
	if len(got) != 1 || got[0] != "key-wildcard" {
		t.Errorf("remaining TXT values = %v, want [key-wildcard]", got)
	}
}

// TestCleanUpAbsentIsNoOp asserts CleanUp of a value not present does nothing —
// no blind delete by a guessed index.
func TestCleanUpAbsentIsNoOp(t *testing.T) {
	mock := newMockSWEB(dns.Record{Index: flex.Int(0), Type: "TXT", Category: "mainTxt", Domain: "@", Value: "someone-else"})
	srv := mock.server(t)
	s := testSolver(t)

	if err := s.CleanUp(challenge(t, srv.URL, "key-missing")); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	if _, del := mock.counts(); del != 0 {
		t.Errorf("delCount = %d, want 0 (nothing matched)", del)
	}
}

// TestPresentConcurrentSameZone drives two Present calls at once on the same zone
// (as cert-manager does for base+wildcard SANs). With -race this exercises the
// per-zone mutex; both records must land exactly once.
func TestPresentConcurrentSameZone(t *testing.T) {
	mock := newMockSWEB()
	srv := mock.server(t)
	s := testSolver(t)

	var wg sync.WaitGroup
	for _, key := range []string{"key-base", "key-wildcard"} {
		wg.Add(1)
		go func(k string) {
			defer wg.Done()
			if err := s.Present(challenge(t, srv.URL, k)); err != nil {
				t.Errorf("Present(%s): %v", k, err)
			}
		}(key)
	}
	wg.Wait()

	if add, _ := mock.counts(); add != 2 {
		t.Errorf("addCount = %d, want 2", add)
	}
	if got := len(mock.txtValues()); got != 2 {
		t.Errorf("zone TXT count = %d, want 2", got)
	}
}

package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/sanchpet/sweb-go-sdk/dns"
)

// solverName is the ACME solver name within the API group. It must equal the
// Issuer's dns01.webhook.solverName.
const solverName = "spaceweb"

// spacewebSolver implements webhook.Solver for the SpaceWeb DNS provider.
//
// SpaceWeb's DNS API has two quirks that a naive fork of webhook-example gets
// wrong, so both are handled here explicitly:
//   - "add" is not idempotent — re-adding an identical TXT duplicates it — so
//     Present lists first and only adds when the (name,value) pair is absent.
//   - a record is deleted by its per-category positional index, which "add"
//     never returns, so CleanUp lists, matches the record by its value, and
//     removes it by that record's own index (never a guessed one).
//
// cert-manager drives challenges concurrently: a certificate with a base SAN and
// a wildcard SAN presents two _acme-challenge TXTs on the same zone at once. The
// list-then-mutate sequences above race, so each is serialised under a per-zone
// mutex.
type spacewebSolver struct {
	// client reads the credential Secret referenced by each Issuer's config. Set
	// once in Initialize.
	client kubernetes.Interface

	// zoneLocks serialises Present/CleanUp per zone (domain). locksMu guards the
	// map itself.
	locksMu   sync.Mutex
	zoneLocks map[string]*sync.Mutex
}

// Name returns the solver name; it must match the Issuer's solverName.
func (s *spacewebSolver) Name() string { return solverName }

// Initialize builds the kubernetes clientset used to read credential Secrets. It
// is invoked as an apiserver post-start hook.
func (s *spacewebSolver) Initialize(kubeClientConfig *rest.Config, _ <-chan struct{}) error {
	cl, err := kubernetes.NewForConfig(kubeClientConfig)
	if err != nil {
		return fmt.Errorf("building kubernetes clientset: %w", err)
	}
	s.client = cl
	return nil
}

// Present publishes the challenge TXT record (value ch.Key) for the resolved
// FQDN. It is a no-op when an identical record already exists, because SpaceWeb's
// "add" is not idempotent.
func (s *spacewebSolver) Present(ch *v1alpha1.ChallengeRequest) error {
	ctx := context.TODO()

	cfg, err := loadConfig(ch.Config)
	if err != nil {
		return err
	}
	client, err := s.newSWEBClient(ctx, ch, cfg)
	if err != nil {
		return err
	}
	domain, subDomain := splitFQDN(ch)

	lock := s.zoneLock(domain)
	lock.Lock()
	defer lock.Unlock()

	records, err := client.DNS.Records(ctx, domain)
	if err != nil {
		return fmt.Errorf("listing DNS records for %q: %w", domain, err)
	}
	for _, r := range records {
		if isChallengeTXT(r, subDomain, ch.Key) {
			// Already present. Adding again would duplicate it.
			return nil
		}
	}

	// EditTXT add ignores the index argument (0) — the record's index is assigned
	// by the API, not the caller.
	if err := client.DNS.EditTXT(ctx, domain, dns.ActionAdd, 0, subDomain, ch.Key); err != nil {
		return fmt.Errorf("adding TXT record %q in zone %q: %w", subDomain, domain, err)
	}
	return nil
}

// CleanUp removes the challenge TXT record. It lists the zone, finds the TXT
// whose value equals ch.Key, and removes it by that record's own positional
// index — never a guessed one, since the index is per-category positional and
// not returned by "add".
func (s *spacewebSolver) CleanUp(ch *v1alpha1.ChallengeRequest) error {
	ctx := context.TODO()

	cfg, err := loadConfig(ch.Config)
	if err != nil {
		return err
	}
	client, err := s.newSWEBClient(ctx, ch, cfg)
	if err != nil {
		return err
	}
	domain, _ := splitFQDN(ch)

	lock := s.zoneLock(domain)
	lock.Lock()
	defer lock.Unlock()

	records, err := client.DNS.Records(ctx, domain)
	if err != nil {
		return fmt.Errorf("listing DNS records for %q: %w", domain, err)
	}
	for _, r := range records {
		if r.Type == "TXT" && r.Value == ch.Key {
			// EditTXT remove addresses the record by index; subDomain/value are
			// ignored by the delete wire shape.
			if err := client.DNS.EditTXT(ctx, domain, dns.ActionRemove, int(r.Index), "", ch.Key); err != nil {
				return fmt.Errorf("removing TXT record index %d in zone %q: %w", int(r.Index), domain, err)
			}
			return nil
		}
	}
	// Record already gone — nothing to do.
	return nil
}

// splitFQDN derives the SpaceWeb (domain, subDomain) pair from the challenge.
// For ResolvedFQDN "_acme-challenge.example.com." within ResolvedZone
// "example.com." it yields ("example.com", "_acme-challenge").
func splitFQDN(ch *v1alpha1.ChallengeRequest) (domain, subDomain string) {
	subDomain = strings.TrimSuffix(ch.ResolvedFQDN, "."+ch.ResolvedZone)
	subDomain = strings.TrimSuffix(subDomain, ".")
	domain = strings.TrimSuffix(ch.ResolvedZone, ".")
	return domain, subDomain
}

// isChallengeTXT reports whether record r is the challenge TXT for subDomain with
// the given value.
func isChallengeTXT(r dns.Record, subDomain, value string) bool {
	return r.Type == "TXT" && txtSubDomain(r) == subDomain && r.Value == value
}

// txtSubDomain returns a TXT record's subdomain relative to the zone. SpaceWeb
// reports the apex as "@"; normalise it to "" to match a computed apex
// subDomain.
func txtSubDomain(r dns.Record) string {
	if r.Domain == "@" {
		return ""
	}
	return r.Domain
}

// zoneLock returns the mutex serialising mutations of the given zone, creating it
// on first use.
func (s *spacewebSolver) zoneLock(zone string) *sync.Mutex {
	s.locksMu.Lock()
	defer s.locksMu.Unlock()
	if s.zoneLocks == nil {
		s.zoneLocks = make(map[string]*sync.Mutex)
	}
	lock, ok := s.zoneLocks[zone]
	if !ok {
		lock = &sync.Mutex{}
		s.zoneLocks[zone] = lock
	}
	return lock
}

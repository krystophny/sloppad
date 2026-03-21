package web

import (
	"net"
	"os"
	"strings"

	"github.com/grandcat/zeroconf"
)

const (
	taburaMDNSService = "_tabura._tcp"
	taburaMDNSDomain  = "local."
	taburaMDNSVersion = "0.2.1"
)

type mdnsAdvertiser interface {
	Shutdown()
}

func defaultMDNSAdvertiserFactory(name string, port int, txt []string) (mdnsAdvertiser, error) {
	return zeroconf.Register(name, taburaMDNSService, taburaMDNSDomain, port, txt, nil)
}

func (a *App) startMDNSAdvertisement(host string, port int) error {
	if a == nil || a.mdnsAdvertiserFactory == nil || port <= 0 {
		return nil
	}
	if !hostAllowsLANAdvertisement(host) {
		return nil
	}
	if a.mdnsAdvertiser != nil {
		a.mdnsAdvertiser.Shutdown()
		a.mdnsAdvertiser = nil
	}
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		hostname = "tabura"
	}
	instance := "tabura-" + sanitizeMDNSLabel(hostname)
	advertiser, err := a.mdnsAdvertiserFactory(instance, port, []string{
		"version=" + taburaMDNSVersion,
		"hostname=" + hostname,
	})
	if err != nil {
		return err
	}
	a.mdnsAdvertiser = advertiser
	return nil
}

func (a *App) stopMDNSAdvertisement() {
	if a == nil || a.mdnsAdvertiser == nil {
		return
	}
	a.mdnsAdvertiser.Shutdown()
	a.mdnsAdvertiser = nil
}

func hostAllowsLANAdvertisement(host string) bool {
	clean := strings.TrimSpace(strings.ToLower(host))
	switch clean {
	case "", "0.0.0.0", "::":
		return true
	case "localhost", "127.0.0.1", "::1":
		return false
	}
	if ip := net.ParseIP(clean); ip != nil {
		return !ip.IsLoopback()
	}
	return true
}

func sanitizeMDNSLabel(raw string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(strings.ToLower(raw)) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	label := strings.Trim(b.String(), "-")
	if label == "" {
		return "tabura"
	}
	return label
}

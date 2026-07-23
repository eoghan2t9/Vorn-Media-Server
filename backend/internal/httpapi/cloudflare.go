package httpapi

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

// cloudflareIPRanges holds the CIDR ranges Cloudflare's edge connects from.
// Cloudflare's own docs note this list does change over time, so it's
// refreshed periodically from Cloudflare's public API
// (https://api.cloudflare.com/client/v4/ips) rather than hardcoded; a
// baked-in snapshot is used only until the first successful refresh (or if
// that never succeeds, e.g. no outbound internet at startup).
type cloudflareIPRanges struct {
	mu     sync.RWMutex
	ranges []*net.IPNet
}

var cfRanges = &cloudflareIPRanges{ranges: parseCIDRs(fallbackCloudflareCIDRs)}

func startCloudflareRangeRefresh() {
	go func() {
		cfRanges.refresh()
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			cfRanges.refresh()
		}
	}()
}

func (c *cloudflareIPRanges) refresh() {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://api.cloudflare.com/client/v4/ips")
	if err != nil {
		log.Printf("httpapi: refreshing cloudflare ip ranges: %v", err)
		return
	}
	defer resp.Body.Close()

	var body struct {
		Success bool `json:"success"`
		Result  struct {
			IPv4CIDRs []string `json:"ipv4_cidrs"`
			IPv6CIDRs []string `json:"ipv6_cidrs"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || !body.Success {
		log.Printf("httpapi: parsing cloudflare ip ranges response: %v", err)
		return
	}

	all := append(append([]string{}, body.Result.IPv4CIDRs...), body.Result.IPv6CIDRs...)
	parsed := parseCIDRs(all)
	if len(parsed) == 0 {
		return
	}
	c.mu.Lock()
	c.ranges = parsed
	c.mu.Unlock()
}

func (c *cloudflareIPRanges) contains(ip net.IP) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, n := range c.ranges {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func parseCIDRs(cidrs []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, raw := range cidrs {
		if _, n, err := net.ParseCIDR(raw); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// fallbackCloudflareCIDRs is a snapshot as of 2026-07 (from
// https://www.cloudflare.com/ips-v4 and /ips-v6), used only as a seed until
// the first live refresh succeeds.
var fallbackCloudflareCIDRs = []string{
	"173.245.48.0/20",
	"103.21.244.0/22",
	"103.22.200.0/22",
	"103.31.4.0/22",
	"141.101.64.0/18",
	"108.162.192.0/18",
	"190.93.240.0/20",
	"188.114.96.0/20",
	"197.234.240.0/22",
	"198.41.128.0/17",
	"162.158.0.0/15",
	"104.16.0.0/13",
	"104.24.0.0/14",
	"172.64.0.0/13",
	"131.0.72.0/22",
	"2400:cb00::/32",
	"2606:4700::/32",
	"2803:f800::/32",
	"2405:8100::/32",
	"2a06:98c0::/29",
	"2c0f:f248::/32",
}

// realClientIP returns the IP address a request should be attributed to:
// CF-Connecting-IP if trustCloudflare is enabled AND the actual TCP peer is
// a genuine Cloudflare edge IP (so the header can't simply be spoofed by
// any client that feels like setting it), otherwise the raw RemoteAddr.
func realClientIP(r *http.Request, trustCloudflare bool) string {
	remoteIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteIP = r.RemoteAddr
	}
	if !trustCloudflare {
		return remoteIP
	}

	peer := net.ParseIP(remoteIP)
	if peer == nil || !cfRanges.contains(peer) {
		return remoteIP
	}
	if cfIP := r.Header.Get("CF-Connecting-IP"); cfIP != "" {
		return cfIP
	}
	return remoteIP
}

package main

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

// Public base domain handling.
//
// The proxy is untrusted (see .cursor/rules/NEVER-TRUST-PROXY.md). The domain
// a sidecar is published under — https://<fingerprint>.<baseDomain> — is a
// security-relevant value: it ends up in Headscale's server_url and in the QR
// codes clients pin. So it is derived here, locally, from what the operator
// configured (--proxy-domain, or else the host of --proxy). Nothing the proxy
// returns is ever used to choose it; proxy-returned URLs are only checked
// against it.

// defaultProxyURL is the public HeadFwd relay.
const defaultProxyURL = "https://headfwd.net"

var (
	dnsLabelRe   = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	fingerprintR = regexp.MustCompile(`^[a-f0-9]{32}$`)
)

// resolveProxyDomain returns the public base domain this sidecar expects to be
// served under. An explicit override wins; otherwise it is the host of
// proxyURL. The result is lowercased, with any port and trailing dot removed.
func resolveProxyDomain(proxyURL, override string) (string, error) {
	var host string
	if override != "" {
		if strings.ContainsAny(override, "/:@*?#") {
			return "", fmt.Errorf("invalid --proxy-domain %q: pass a bare host such as --proxy-domain example.com (no scheme, port, path or wildcard)", override)
		}
		host = override
	} else {
		u, err := url.Parse(proxyURL)
		if err != nil {
			return "", fmt.Errorf("invalid --proxy URL %q: %w", proxyURL, err)
		}
		if u.User != nil {
			return "", fmt.Errorf("invalid --proxy URL %q: userinfo is not allowed", proxyURL)
		}
		host = u.Hostname()
		if host == "" {
			return "", fmt.Errorf("invalid --proxy URL %q: no host (expected e.g. https://example.com); or pass --proxy-domain example.com", proxyURL)
		}
	}

	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "" {
		return "", fmt.Errorf("public base domain is empty; pass --proxy-domain example.com")
	}
	if net.ParseIP(host) != nil {
		return "", fmt.Errorf("public base domain %q is an IP address; <fingerprint>.<domain> needs a DNS name. If you reach the proxy by IP, pass --proxy-domain example.com", host)
	}
	if len(host) > 253 {
		return "", fmt.Errorf("public base domain %q is too long", host)
	}
	for _, label := range strings.Split(host, ".") {
		if !dnsLabelRe.MatchString(label) {
			return "", fmt.Errorf("public base domain %q is not a valid DNS name; pass a bare host such as --proxy-domain example.com", host)
		}
	}
	return host, nil
}

// publicURLFromFingerprint builds the public URL for a fingerprint under the
// locally configured base domain.
func publicURLFromFingerprint(fingerprint, baseDomain string) string {
	if fingerprint == "" || baseDomain == "" {
		return ""
	}
	return "https://" + fingerprint + "." + baseDomain
}

// verifyRegistrationURLs checks the proxy's verify response against values
// derived locally: the fingerprint from Headscale's Noise key and the base
// domain from configuration. The proxy's URLs are accepted only if they point
// at exactly https/wss://<localFP>.<baseDomain>.
func verifyRegistrationURLs(localFP, baseDomain string, resp RegistrationVerifyResponse) error {
	if resp.Fingerprint != localFP {
		return fmt.Errorf("proxy verify fingerprint mismatch: local=%s proxy=%s", localFP, resp.Fingerprint)
	}
	expectedHost := localFP + "." + baseDomain

	pub, err := url.Parse(resp.PublicURL)
	if err != nil {
		return fmt.Errorf("proxy public URL is not a valid URL: %q", resp.PublicURL)
	}
	if err := checkIssuedURL("public", pub, "https", expectedHost, baseDomain); err != nil {
		return err
	}
	if pub.Path != "" && pub.Path != "/" || pub.RawQuery != "" || pub.Fragment != "" {
		return fmt.Errorf("proxy public URL mismatch: expected=%s got=%s", publicURLFromFingerprint(localFP, baseDomain), resp.PublicURL)
	}

	tun, err := url.Parse(resp.TunnelURL)
	if err != nil {
		return fmt.Errorf("proxy tunnel URL is not a valid URL")
	}
	if err := checkIssuedURL("tunnel", tun, "wss", expectedHost, baseDomain); err != nil {
		return err
	}
	return nil
}

func checkIssuedURL(kind string, u *url.URL, scheme, expectedHost, baseDomain string) error {
	if !strings.EqualFold(u.Scheme, scheme) {
		return fmt.Errorf("proxy %s URL has scheme %q, expected %q", kind, u.Scheme, scheme)
	}
	if u.User != nil {
		return fmt.Errorf("proxy %s URL contains userinfo; refusing", kind)
	}
	if u.Port() != "" {
		return fmt.Errorf("proxy %s URL has an explicit port (%s); expected %s", kind, u.Port(), expectedHost)
	}
	host := u.Hostname()
	if strings.EqualFold(host, expectedHost) {
		return nil
	}
	// Give an actionable error when the proxy is simply serving a different
	// domain (usually a PUBLIC_HOST misconfiguration). The proxy's domain is
	// reported, never adopted.
	if i := strings.IndexByte(host, '.'); i > 0 {
		if claimed := strings.ToLower(host[i+1:]); claimed != baseDomain {
			return fmt.Errorf(
				"proxy issued %s URL on %q but this sidecar expects %q (from --proxy / --proxy-domain). "+
					"If you run that proxy, set PUBLIC_HOST=%s on it; otherwise check --proxy / --proxy-domain",
				kind, claimed, baseDomain, baseDomain)
		}
	}
	return fmt.Errorf("proxy %s URL host mismatch: expected=%s got=%s", kind, expectedHost, host)
}

// isHeadfwdServerURL reports whether a Headscale server_url looks like one
// HeadFwd wrote earlier: https://<32-hex fingerprint>.<anything>. Such a value
// may be rewritten on re-registration (e.g. when moving to another proxy
// domain); anything else is the operator's own setting and is left alone
// unless --force-update is given.
func isHeadfwdServerURL(current string) bool {
	u, err := url.Parse(strings.TrimSpace(current))
	if err != nil || u.Hostname() == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	i := strings.IndexByte(host, '.')
	if i <= 0 || i == len(host)-1 {
		return false
	}
	return fingerprintR.MatchString(host[:i])
}

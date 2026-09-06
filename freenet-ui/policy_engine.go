package main

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

// PolicySelectorKind identifies the namespace used to match traffic.
// Domain-aware selectors can be shared by payload routing and Split DNS.
type PolicySelectorKind string

const (
	PolicySelectorDomain  PolicySelectorKind = "domain"
	PolicySelectorGeoSite PolicySelectorKind = "geosite"
	PolicySelectorIP      PolicySelectorKind = "ip"
	PolicySelectorCIDR    PolicySelectorKind = "cidr"
	PolicySelectorGeoIP   PolicySelectorKind = "geoip"
)

// PolicyAction is the user-visible routing decision.
type PolicyAction string

const (
	PolicyActionDirect PolicyAction = "DIRECT"
	PolicyActionVPN    PolicyAction = "VPN"
	PolicyActionBlock  PolicyAction = "BLOCK"
)

// PolicySelector is the declarative selector stored in the FreeNet policy model.
type PolicySelector struct {
	Kind  PolicySelectorKind `json:"kind"`
	Value string             `json:"value"`
}

// PolicyRule is one ordered user policy rule. Input order is first-match order.
type PolicyRule struct {
	Selector PolicySelector `json:"selector"`
	Action   PolicyAction   `json:"action"`
}

// CompiledPolicyRule is the normalized intermediate representation consumed by
// future Xray renderers. DNSLeg is intentionally empty for IP-based selectors.
type CompiledPolicyRule struct {
	Order           int                `json:"order"`
	Selector        PolicySelector     `json:"selector"`
	Action          PolicyAction       `json:"action"`
	PayloadOutbound string             `json:"payload_outbound"`
	DNSLeg          string             `json:"dns_leg,omitempty"`
}

// CompiledPolicy contains one canonical ordered rule set plus explicit views for
// payload and Split DNS. It performs no runtime mutation and contains no secrets.
type CompiledPolicy struct {
	Rules   []CompiledPolicyRule `json:"rules"`
	Payload []CompiledPolicyRule `json:"payload"`
	DNS     []CompiledPolicyRule `json:"dns"`
}

var policyCategoryPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._@:-]*$`)

// CompilePolicy validates and normalizes a declarative ordered policy.
// domain/geosite participate in both payload routing and Split DNS;
// ip/cidr/geoip participate only in payload routing because destination IP is
// not available before DNS resolution.
func CompilePolicy(input []PolicyRule) (CompiledPolicy, error) {
	out := CompiledPolicy{
		Rules:   make([]CompiledPolicyRule, 0, len(input)),
		Payload: make([]CompiledPolicyRule, 0, len(input)),
		DNS:     make([]CompiledPolicyRule, 0, len(input)),
	}
	seen := make(map[string]struct{}, len(input))

	for i, raw := range input {
		selector, err := normalizePolicySelector(raw.Selector)
		if err != nil {
			return CompiledPolicy{}, fmt.Errorf("rule %d: %w", i+1, err)
		}
		action, err := normalizePolicyAction(raw.Action)
		if err != nil {
			return CompiledPolicy{}, fmt.Errorf("rule %d: %w", i+1, err)
		}

		key := string(selector.Kind) + "\x00" + selector.Value
		if _, ok := seen[key]; ok {
			return CompiledPolicy{}, fmt.Errorf("rule %d: duplicate selector %s:%s", i+1, selector.Kind, selector.Value)
		}
		seen[key] = struct{}{}

		compiled := CompiledPolicyRule{
			Order:           i,
			Selector:        selector,
			Action:          action,
			PayloadOutbound: payloadOutboundForAction(action),
		}
		if policySelectorHasDNSLeg(selector.Kind) {
			compiled.DNSLeg = dnsLegForAction(action)
		}

		out.Rules = append(out.Rules, compiled)
		out.Payload = append(out.Payload, compiled)
		if compiled.DNSLeg != "" {
			out.DNS = append(out.DNS, compiled)
		}
	}

	return out, nil
}

func normalizePolicyAction(action PolicyAction) (PolicyAction, error) {
	normalized := PolicyAction(strings.ToUpper(strings.TrimSpace(string(action))))
	switch normalized {
	case PolicyActionDirect, PolicyActionVPN, PolicyActionBlock:
		return normalized, nil
	default:
		return "", fmt.Errorf("unsupported action %q", action)
	}
}

func normalizePolicySelector(selector PolicySelector) (PolicySelector, error) {
	kind := PolicySelectorKind(strings.ToLower(strings.TrimSpace(string(selector.Kind))))
	value := strings.TrimSpace(selector.Value)
	if value == "" {
		return PolicySelector{}, fmt.Errorf("empty selector value")
	}

	switch kind {
	case PolicySelectorDomain:
		normalized, err := normalizePolicyDomain(value)
		if err != nil {
			return PolicySelector{}, err
		}
		return PolicySelector{Kind: kind, Value: normalized}, nil
	case PolicySelectorGeoSite, PolicySelectorGeoIP:
		normalized := strings.ToLower(value)
		if !policyCategoryPattern.MatchString(normalized) {
			return PolicySelector{}, fmt.Errorf("invalid %s category %q", kind, value)
		}
		return PolicySelector{Kind: kind, Value: normalized}, nil
	case PolicySelectorIP:
		ip := net.ParseIP(value)
		if ip == nil {
			return PolicySelector{}, fmt.Errorf("invalid IP %q", value)
		}
		return PolicySelector{Kind: kind, Value: ip.String()}, nil
	case PolicySelectorCIDR:
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return PolicySelector{}, fmt.Errorf("invalid CIDR %q", value)
		}
		return PolicySelector{Kind: kind, Value: network.String()}, nil
	default:
		return PolicySelector{}, fmt.Errorf("unsupported selector kind %q", selector.Kind)
	}
}

func normalizePolicyDomain(value string) (string, error) {
	domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
	if domain == "" || len(domain) > 253 || strings.ContainsAny(domain, " /\\:@") {
		return "", fmt.Errorf("invalid domain %q", value)
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return "", fmt.Errorf("invalid domain %q", value)
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", fmt.Errorf("invalid domain %q", value)
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' && r != '_' {
				return "", fmt.Errorf("invalid domain %q", value)
			}
		}
	}
	return domain, nil
}

func policySelectorHasDNSLeg(kind PolicySelectorKind) bool {
	return kind == PolicySelectorDomain || kind == PolicySelectorGeoSite
}

func payloadOutboundForAction(action PolicyAction) string {
	switch action {
	case PolicyActionDirect:
		return "direct"
	case PolicyActionVPN:
		return "vless-reality"
	case PolicyActionBlock:
		return "block"
	default:
		return ""
	}
}

func dnsLegForAction(action PolicyAction) string {
	switch action {
	case PolicyActionDirect:
		return "dns-direct"
	case PolicyActionVPN:
		return "dns-vless"
	case PolicyActionBlock:
		return "block"
	default:
		return ""
	}
}

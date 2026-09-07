package model

import "strings"

const (
	ModeStandard = "standard"
	ModeFast     = "fast"
	ModeUnknown  = "unknown"
)

type ServiceMode struct {
	ServiceMode string `json:"service_mode"`
	ServiceTier string `json:"service_tier,omitempty"`
	ModeSource  string `json:"mode_source"`
	ModeAssumed bool   `json:"mode_assumed"`
}

func ModeFromTier(tier, source string) ServiceMode {
	m := ServiceMode{ServiceMode: ModeUnknown, ServiceTier: tier, ModeSource: source, ModeAssumed: true}
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "fast", "priority":
		m.ServiceMode, m.ModeAssumed = ModeFast, false
	case "default":
		m.ServiceMode, m.ModeAssumed = ModeStandard, false
	}
	return m
}

func (m ServiceMode) Normalized() ServiceMode {
	if m.ServiceMode != ModeFast && m.ServiceMode != ModeStandard {
		m.ServiceMode = ModeUnknown
	}
	if m.ModeSource == "" {
		m.ModeSource = "unavailable"
	}
	m.ModeAssumed = m.ServiceMode == ModeUnknown
	return m
}

// Unknown is a subset of Regular, never a third additive bucket.
type ModeUsage struct {
	Regular TokenUsage `json:"regular"`
	Fast    TokenUsage `json:"fast"`
	Unknown TokenUsage `json:"unknown"`
}

func (m *ModeUsage) Add(usage TokenUsage, mode string) {
	if mode == ModeFast {
		m.Fast = m.Fast.Add(usage)
		return
	}
	m.Regular = m.Regular.Add(usage)
	if mode != ModeStandard {
		m.Unknown = m.Unknown.Add(usage)
	}
}

func (m *ModeUsage) Complete(total TokenUsage) { m.Regular = total.Sub(m.Fast) }

package model

import "strings"

const (
	ModeStandard  = "standard"
	ModeFast      = "fast"
	ModeUltrafast = "ultrafast"
	ModeUnknown   = "unknown"
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
	case ModeUltrafast:
		m.ServiceMode, m.ModeAssumed = ModeUltrafast, false
	case "default":
		m.ServiceMode, m.ModeAssumed = ModeStandard, false
	}
	return m
}

func (m ServiceMode) Normalized() ServiceMode {
	if m.ServiceMode != ModeFast && m.ServiceMode != ModeUltrafast && m.ServiceMode != ModeStandard {
		m.ServiceMode = ModeUnknown
	}
	if m.ModeSource == "" {
		m.ModeSource = "unavailable"
	}
	m.ModeAssumed = m.ServiceMode == ModeUnknown
	return m
}

// Regular + Fast accounts for all usage. Unknown is a subset of Regular,
// and Ultrafast is a subset of the accelerated (Fast) total.
type ModeUsage struct {
	Regular   TokenUsage `json:"regular"`
	Fast      TokenUsage `json:"fast"`
	Ultrafast TokenUsage `json:"ultrafast"`
	Unknown   TokenUsage `json:"unknown"`
}

func (m *ModeUsage) Add(usage TokenUsage, mode string) {
	if mode == ModeFast || mode == ModeUltrafast {
		m.Fast = m.Fast.Add(usage)
		if mode == ModeUltrafast {
			m.Ultrafast = m.Ultrafast.Add(usage)
		}
		return
	}
	m.Regular = m.Regular.Add(usage)
	if mode != ModeStandard {
		m.Unknown = m.Unknown.Add(usage)
	}
}

func (m *ModeUsage) Complete(total TokenUsage) { m.Regular = total.Sub(m.Fast) }

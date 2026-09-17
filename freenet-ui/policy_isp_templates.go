package main

import (
	"errors"
	"net/http"
)

type policyISPPresetTemplate struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	YouTubeRoute string         `json:"youtube_route"`
	Rules        []PolicyRule   `json:"rules"`
	Compiled     CompiledPolicy `json:"compiled"`
	Message      string         `json:"message,omitempty"`
}

type policyISPPresetsResponse struct {
	Success  bool                      `json:"success"`
	Mutation string                    `json:"mutation"`
	Presets  []policyISPPresetTemplate `json:"presets"`
	Error    string                    `json:"error,omitempty"`
}

type policyISPPresetSpec struct {
	ID           string
	YouTubeRoute string
	Rules        []PolicyRule
	Message      string
}

var policyISPPresetSpecs = []policyISPPresetSpec{
	{
		ID:           "auto",
		YouTubeRoute: "detect",
		Message:      "Авто не создаёт routing policy без runtime-факта; mutation: NONE.",
	},
	{
		ID:           "vladlink",
		YouTubeRoute: "direct",
		Rules: []PolicyRule{{
			Selector: PolicySelector{Kind: PolicySelectorGeoSite, Value: "youtube"},
			Action:   PolicyActionDirect,
		}},
		Message: "Владлинк baseline: YouTube → DIRECT.",
	},
	{
		ID:           "alliancetelecom",
		YouTubeRoute: "direct",
		Rules: []PolicyRule{{
			Selector: PolicySelector{Kind: PolicySelectorGeoSite, Value: "youtube"},
			Action:   PolicyActionDirect,
		}},
		Message: "АльянсТелеком baseline: YouTube → DIRECT.",
	},
	{
		ID:           "rostelecom",
		YouTubeRoute: "vpn",
		Rules: []PolicyRule{{
			Selector: PolicySelector{Kind: PolicySelectorGeoSite, Value: "youtube"},
			Action:   PolicyActionVPN,
		}},
		Message: "Ростелеком baseline: YouTube → VPN.",
	},
	{
		ID:           "podryad",
		YouTubeRoute: "detect",
		Message:      "Подряд — отдельный ISP profile; FreeNet не наследует чужую policy без подтверждения. Mutation: NONE.",
	},
	{
		ID:           "custom",
		YouTubeRoute: "custom",
		Message:      "Custom — ручная экспертная policy; FreeNet не создаёт автоматический template.",
	},
}

func buildPolicyISPPresets() ([]policyISPPresetTemplate, error) {
	presets := make([]policyISPPresetTemplate, 0, len(policyISPPresetSpecs))
	for _, spec := range policyISPPresetSpecs {
		meta, ok := ispProfiles[spec.ID]
		if !ok {
			return nil, errors.New("policy ISP preset references unknown ISP")
		}
		rules := append([]PolicyRule{}, spec.Rules...)
		compiled, err := CompilePolicy(rules)
		if err != nil {
			return nil, err
		}
		presets = append(presets, policyISPPresetTemplate{
			ID:           spec.ID,
			Name:         meta.Label,
			YouTubeRoute: spec.YouTubeRoute,
			Rules:        rules,
			Compiled:     compiled,
			Message:      spec.Message,
		})
	}
	return presets, nil
}

func (a *app) handlePolicyISPPresets(w http.ResponseWriter, _ *http.Request) {
	presets, err := buildPolicyISPPresets()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, policyISPPresetsResponse{Success: false, Mutation: "NONE", Error: "cannot build ISP policy templates"})
		return
	}
	writeJSON(w, http.StatusOK, policyISPPresetsResponse{Success: true, Mutation: "NONE", Presets: presets})
}

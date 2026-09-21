package main

import (
	"errors"
	"net/http"
)

type policyISPPresetTemplate struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Rules    []PolicyRule   `json:"rules"`
	Compiled CompiledPolicy `json:"compiled"`
	Message  string         `json:"message,omitempty"`
}

type policyISPPresetsResponse struct {
	Success  bool                      `json:"success"`
	Mutation string                    `json:"mutation"`
	Presets  []policyISPPresetTemplate `json:"presets"`
	Error    string                    `json:"error,omitempty"`
}

type policyISPPresetSpec struct {
	ID      string
	Message string
}

var policyISPPresetSpecs = []policyISPPresetSpec{
	{
		ID:      "auto",
		Message: "Авто не создаёт routing policy без явного пользовательского правила или подтверждённого runtime-факта; mutation: NONE.",
	},
	{
		ID:      "vladlink",
		Message: "Владлинк: ISP profile хранит сетевые метаданные; routing policy задаётся явно, без YouTube baseline.",
	},
	{
		ID:      "alliancetelecom",
		Message: "АльянсТелеком: ISP profile хранит сетевые метаданные; routing policy задаётся явно, без YouTube baseline.",
	},
	{
		ID:      "rostelecom",
		Message: "Ростелеком: ISP profile хранит сетевые метаданные; routing policy задаётся явно, без YouTube baseline.",
	},
	{
		ID:      "podryad",
		Message: "Подряд: отдельный ISP profile; FreeNet не наследует чужую routing policy и не создаёт YouTube baseline.",
	},
	{
		ID:      "custom",
		Message: "Custom: ручная экспертная policy; FreeNet не создаёт автоматический routing template.",
	},
}

func buildPolicyISPPresets() ([]policyISPPresetTemplate, error) {
	presets := make([]policyISPPresetTemplate, 0, len(policyISPPresetSpecs))
	for _, spec := range policyISPPresetSpecs {
		meta, ok := ispProfiles[spec.ID]
		if !ok {
			return nil, errors.New("policy ISP preset references unknown ISP")
		}
		rules := []PolicyRule{}
		compiled, err := CompilePolicy(rules)
		if err != nil {
			return nil, err
		}
		presets = append(presets, policyISPPresetTemplate{
			ID:       spec.ID,
			Name:     meta.Label,
			Rules:    rules,
			Compiled: compiled,
			Message:  spec.Message,
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

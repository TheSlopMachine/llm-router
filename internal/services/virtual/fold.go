package virtual

import (
	"github.com/TheSlopMachine/llm-router/internal/models"
)

// Metadata is generated virtual-model metadata, folded serve-time from the
// currently available members. Every set is an intersection (only what all
// members share); limits are minima over members reporting them.
type Metadata struct {
	Capabilities        []string
	ContextLength       int64
	MaxCompletionTokens int64
	InputModalities     []string
	OutputModalities    []string
	Reasoning           *models.ModelReasoning
	SupportedParameters []string
}

// intersectStrings keeps elements of first present in every other list, in first's order.
func intersectStrings(first []string, rest ...[]string) []string {
	out := make([]string, 0, len(first))
	for _, v := range first {
		keep := true
		for _, o := range rest {
			found := false
			for _, x := range o {
				if x == v {
					found = true
					break
				}
			}
			if !found {
				keep = false
				break
			}
		}
		if keep {
			out = append(out, v)
		}
	}
	return out
}

// minPositive returns the smallest positive value, or 0 when none is positive.
func minPositive(vals ...int64) int64 {
	min := int64(0)
	for _, v := range vals {
		if v <= 0 {
			continue
		}
		if min == 0 || v < min {
			min = v
		}
	}
	return min
}

// FoldMembers folds member infos into one virtual metadata record.
// Callers pass only available (known, enabled) members; an empty input
// yields a zero Metadata.
func FoldMembers(members []models.ModelInfo) Metadata {
	var agg Metadata
	if len(members) == 0 {
		return agg
	}
	caps := append([]string(nil), members[0].Capabilities...)
	params := append([]string(nil), members[0].SupportedParameters...)
	ins := append([]string(nil), members[0].InputModalities...)
	outs := append([]string(nil), members[0].OutputModalities...)
	ctx := members[0].ContextWindow
	maxTok := members[0].MaxTokens
	allReasoning := members[0].Reasoning != nil
	var efforts []string
	defaultEffort := ""
	defaultEffortSame := true
	defaultEnabled := true
	mandatory := true
	if allReasoning {
		efforts = append([]string(nil), members[0].Reasoning.SupportedEfforts...)
		defaultEffort = members[0].Reasoning.DefaultEffort
		defaultEnabled = members[0].Reasoning.DefaultEnabled
		mandatory = members[0].Reasoning.Mandatory
	}
	for _, m := range members[1:] {
		caps = intersectStrings(caps, m.Capabilities)
		params = intersectStrings(params, m.SupportedParameters)
		ins = intersectStrings(ins, m.InputModalities)
		outs = intersectStrings(outs, m.OutputModalities)
		ctx = minPositive(ctx, m.ContextWindow)
		maxTok = minPositive(maxTok, m.MaxTokens)
		if m.Reasoning == nil {
			allReasoning = false
			continue
		}
		efforts = intersectStrings(efforts, m.Reasoning.SupportedEfforts)
		if defaultEffort != m.Reasoning.DefaultEffort {
			defaultEffortSame = false
		}
		defaultEnabled = defaultEnabled && m.Reasoning.DefaultEnabled
		mandatory = mandatory && m.Reasoning.Mandatory
	}
	agg.Capabilities = caps
	agg.SupportedParameters = params
	agg.InputModalities = ins
	agg.OutputModalities = outs
	agg.ContextLength = ctx
	agg.MaxCompletionTokens = maxTok
	if allReasoning {
		agg.Reasoning = &models.ModelReasoning{
			SupportedEfforts: efforts,
			DefaultEnabled:   defaultEnabled,
			Mandatory:        mandatory,
		}
		if defaultEffortSame {
			agg.Reasoning.DefaultEffort = defaultEffort
		}
	}
	return agg
}

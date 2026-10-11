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
	output := make([]string, 0, len(first))
	for _, value := range first {
		keep := true
		for _, other := range rest {
			found := false
			for _, x := range other {
				if x == value {
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
			output = append(output, value)
		}
	}
	return output
}

// minPositive returns the smallest positive value, or 0 when none is positive.
func minPositive(values ...int64) int64 {
	min := int64(0)
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if min == 0 || value < min {
			min = value
		}
	}
	return min
}

// FoldMembers folds member models into one virtual metadata record.
// Callers pass only available (known, enabled) members; an empty input
// yields a zero Metadata.
func FoldMembers(members []models.ModelInfo) Metadata {
	var aggregate Metadata
	if len(members) == 0 {
		return aggregate
	}
	capabilities := append([]string(nil), members[0].Capabilities...)
	parameters := append([]string(nil), members[0].SupportedParameters...)
	inputs := append([]string(nil), members[0].InputModalities...)
	outputs := append([]string(nil), members[0].OutputModalities...)
	contextLength := members[0].ContextWindow
	maxTokens := members[0].MaxTokens
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
	for _, member := range members[1:] {
		capabilities = intersectStrings(capabilities, member.Capabilities)
		parameters = intersectStrings(parameters, member.SupportedParameters)
		inputs = intersectStrings(inputs, member.InputModalities)
		outputs = intersectStrings(outputs, member.OutputModalities)
		contextLength = minPositive(contextLength, member.ContextWindow)
		maxTokens = minPositive(maxTokens, member.MaxTokens)
		if member.Reasoning == nil {
			allReasoning = false
			continue
		}
		efforts = intersectStrings(efforts, member.Reasoning.SupportedEfforts)
		if defaultEffort != member.Reasoning.DefaultEffort {
			defaultEffortSame = false
		}
		defaultEnabled = defaultEnabled && member.Reasoning.DefaultEnabled
		mandatory = mandatory && member.Reasoning.Mandatory
	}
	aggregate.Capabilities = capabilities
	aggregate.SupportedParameters = parameters
	aggregate.InputModalities = inputs
	aggregate.OutputModalities = outputs
	aggregate.ContextLength = contextLength
	aggregate.MaxCompletionTokens = maxTokens
	if allReasoning {
		aggregate.Reasoning = &models.ModelReasoning{
			SupportedEfforts: efforts,
			DefaultEnabled:   defaultEnabled,
			Mandatory:        mandatory,
		}
		if defaultEffortSame {
			aggregate.Reasoning.DefaultEffort = defaultEffort
		}
	}
	return aggregate
}

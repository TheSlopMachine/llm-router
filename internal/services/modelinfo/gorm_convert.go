package modelinfo

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/TheSlopMachine/llm-router/internal/db/record"
	"github.com/TheSlopMachine/llm-router/internal/models"
)

// Modality directions stored in the modality child rows. The schema CHECK
// pins the same vocabulary; converters never invent a third value.
const (
	modalityDirectionInput  = "input"
	modalityDirectionOutput = "output"
)

// modelOverrideRecordFromModel maps the wire override onto the
// model_overrides row. Capability and modality lists normalize into child
// rows through the helpers below; the base row carries scalars only.
// Records never reach JSON.
func modelOverrideRecordFromModel(override models.ModelOverride) record.ModelOverride {
	return record.ModelOverride{
		ProviderID:  override.ProviderID,
		ModelName:   override.Name,
		Disabled:    override.Disabled,
		Custom:      override.Custom,
		DisplayName: override.DisplayName,
	}
}

// modelOverrideCapabilityRowsFromModel splits override capabilities into
// capability child rows. Values deduplicate and sort so writes stay
// deterministic; the child primary key carries no position.
func modelOverrideCapabilityRowsFromModel(override models.ModelOverride) []record.ModelOverrideCapability {
	values := sortedUniqueStrings(override.Capabilities)
	rows := make([]record.ModelOverrideCapability, 0, len(values))
	for _, capability := range values {
		rows = append(rows, record.ModelOverrideCapability{
			ProviderID: override.ProviderID,
			ModelName:  override.Name,
			Capability: capability,
		})
	}
	return rows
}

// modelOverrideModalityRowsFromModel splits override modalities into
// modality child rows tagged by direction. Values deduplicate and sort per
// direction so writes stay deterministic.
func modelOverrideModalityRowsFromModel(override models.ModelOverride) []record.ModelOverrideModality {
	rows := make([]record.ModelOverrideModality, 0, len(override.InputModalities)+len(override.OutputModalities))
	for _, modality := range sortedUniqueStrings(override.InputModalities) {
		rows = append(rows, record.ModelOverrideModality{
			ProviderID: override.ProviderID,
			ModelName:  override.Name,
			Direction:  modalityDirectionInput,
			Modality:   modality,
		})
	}
	for _, modality := range sortedUniqueStrings(override.OutputModalities) {
		rows = append(rows, record.ModelOverrideModality{
			ProviderID: override.ProviderID,
			ModelName:  override.Name,
			Direction:  modalityDirectionOutput,
			Modality:   modality,
		})
	}
	return rows
}

// modelOverrideFromRecords rebuilds the wire override from the base row and
// its already grouped child lists. Reads arrive ordered from the database,
// so the lists keep their stored order.
func modelOverrideFromRecords(base record.ModelOverride, capabilities, inputModalities, outputModalities []string) *models.ModelOverride {
	return &models.ModelOverride{
		ProviderID:       base.ProviderID,
		Name:             base.ModelName,
		Disabled:         base.Disabled,
		Custom:           base.Custom,
		DisplayName:      base.DisplayName,
		Capabilities:     capabilities,
		InputModalities:  inputModalities,
		OutputModalities: outputModalities,
	}
}

// modelInfoRecordFromModel maps one wire model onto the model_infos row plus
// its capability, modality, and endpoint child rows. Rate limits, windows,
// and display fields map verbatim; SupportedParameters passes through as
// JSON because the vocabulary is upstream-defined; Reasoning marshals to
// JSON for the same reason. Child lists deduplicate and sort so writes stay
// deterministic. Timestamps play no part here: the snapshot header owns
// CachedAt.
func modelInfoRecordFromModel(providerID string, model models.ModelInfo) (record.ModelInfo, []record.ModelInfoCapability, []record.ModelInfoModality, []record.ModelInfoEndpoint, error) {
	reasoning, err := marshalModelReasoning(model.Reasoning)
	if err != nil {
		return record.ModelInfo{}, nil, nil, nil, err
	}
	row := record.ModelInfo{
		ProviderID:          providerID,
		ModelName:           model.Name,
		DisplayName:         model.DisplayName,
		Description:         model.Description,
		RequestsPerMinute:   model.RPM,
		TokensPerMinute:     model.TPM,
		RequestsPerDay:      model.RPD,
		ContextWindow:       model.ContextWindow,
		MaxTokens:           model.MaxTokens,
		SupportedParameters: append([]string(nil), model.SupportedParameters...),
		Reasoning:           reasoning,
	}
	capabilities := make([]record.ModelInfoCapability, 0, len(model.Capabilities))
	for _, capability := range sortedUniqueStrings(model.Capabilities) {
		capabilities = append(capabilities, record.ModelInfoCapability{
			ProviderID: providerID,
			ModelName:  model.Name,
			Capability: capability,
		})
	}
	modalities := make([]record.ModelInfoModality, 0, len(model.InputModalities)+len(model.OutputModalities))
	for _, modality := range sortedUniqueStrings(model.InputModalities) {
		modalities = append(modalities, record.ModelInfoModality{
			ProviderID: providerID,
			ModelName:  model.Name,
			Direction:  modalityDirectionInput,
			Modality:   modality,
		})
	}
	for _, modality := range sortedUniqueStrings(model.OutputModalities) {
		modalities = append(modalities, record.ModelInfoModality{
			ProviderID: providerID,
			ModelName:  model.Name,
			Direction:  modalityDirectionOutput,
			Modality:   modality,
		})
	}
	endpoints := make([]record.ModelInfoEndpoint, 0, len(model.Endpoints))
	for _, endpoint := range sortedUniqueStrings(model.Endpoints) {
		endpoints = append(endpoints, record.ModelInfoEndpoint{
			ProviderID: providerID,
			ModelName:  model.Name,
			Endpoint:   endpoint,
		})
	}
	return row, capabilities, modalities, endpoints, nil
}

// modelInfoFromRecord rebuilds one wire model from its row and already
// grouped child lists. A missing reasoning payload reads as no reasoning;
// a corrupt payload fails loudly instead of dropping the field.
func modelInfoFromRecord(row record.ModelInfo, capabilities, inputModalities, outputModalities, endpoints []string) (models.ModelInfo, error) {
	reasoning, err := unmarshalModelReasoning(row.Reasoning)
	if err != nil {
		return models.ModelInfo{}, err
	}
	return models.ModelInfo{
		Name:                row.ModelName,
		DisplayName:         row.DisplayName,
		Description:         row.Description,
		RPM:                 row.RequestsPerMinute,
		TPM:                 row.TokensPerMinute,
		RPD:                 row.RequestsPerDay,
		ContextWindow:       row.ContextWindow,
		MaxTokens:           row.MaxTokens,
		Capabilities:        capabilities,
		InputModalities:     inputModalities,
		OutputModalities:    outputModalities,
		SupportedParameters: append([]string(nil), row.SupportedParameters...),
		Reasoning:           reasoning,
		Endpoints:           endpoints,
	}, nil
}

// marshalModelReasoning encodes reasoning support for storage. A missing
// value stays missing.
func marshalModelReasoning(reasoning *models.ModelReasoning) ([]byte, error) {
	if reasoning == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(reasoning)
	if err != nil {
		return nil, fmt.Errorf("encode model reasoning: %w", err)
	}
	return encoded, nil
}

// unmarshalModelReasoning decodes stored reasoning support. An empty
// payload reads as no reasoning; corrupt payloads surface an error.
func unmarshalModelReasoning(payload []byte) (*models.ModelReasoning, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	var reasoning models.ModelReasoning
	if err := json.Unmarshal(payload, &reasoning); err != nil {
		return nil, fmt.Errorf("decode model reasoning: %w", err)
	}
	return &reasoning, nil
}

// sortedUniqueStrings deduplicates and sorts values. Empty results read as
// nil so absent lists stay absent instead of empty-but-present.
func sortedUniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	sort.Strings(unique)
	return unique
}

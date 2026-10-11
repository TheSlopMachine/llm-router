// Package token implements the Internal Token Service.
package token

import (
	"fmt"
	"sort"

	"github.com/TheSlopMachine/llm-router/internal/db/record"
	"github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// tokenRecordFromModel maps the base token row. Rules normalize into join
// rows through tokenRulesToJoinRows; the raw secret never persists.
func tokenRecordFromModel(token *models.RouterToken) record.RouterToken {
	return record.RouterToken{
		ID:                  token.ID,
		Name:                token.Name,
		TokenHash:           token.TokenHash,
		AllowAllProviders:   token.Rules.AllowAllProviders,
		AllowAllModels:      token.Rules.AllowAllModels,
		AllowAllCredentials: token.Rules.AllowAllCredentials,
		CreatedAt:           token.CreatedAt,
	}
}

// tokenModelFromRecord rebuilds the wire token from the base row and the
// already assembled rules. The raw secret stays empty outside Create and
// Regenerate.
func tokenModelFromRecord(base record.RouterToken, rules models.TokenRules) *models.RouterToken {
	return &models.RouterToken{
		ID:        base.ID,
		Name:      base.Name,
		TokenHash: base.TokenHash,
		Rules:     rules,
		CreatedAt: base.CreatedAt,
	}
}

// tokenRulesToJoinRows splits TokenRules into the five join tables. List
// values deduplicate and sort so writes stay deterministic. Scopes group by
// provider: duplicate provider entries merge with an OR on the all flag and
// a union on credential identifiers. Scope identifiers generate randomly;
// callers persist every returned slice in one transaction.
func tokenRulesToJoinRows(tokenID string, rules models.TokenRules) ([]record.TokenAllowedProvider, []record.TokenAllowedModel, []record.TokenAllowedCredential, []record.TokenCredentialScope, []record.TokenScopeCredential, error) {
	providerIDs := sortedUniqueStrings(rules.AllowedProviders)
	providers := make([]record.TokenAllowedProvider, 0, len(providerIDs))
	for _, providerID := range providerIDs {
		providers = append(providers, record.TokenAllowedProvider{
			TokenID:    tokenID,
			ProviderID: providerID,
		})
	}

	modelStrings := make([]string, 0, len(rules.AllowedModels))
	for _, modelID := range rules.AllowedModels {
		modelStrings = append(modelStrings, string(modelID))
	}
	modelStrings = sortedUniqueStrings(modelStrings)
	modelRows := make([]record.TokenAllowedModel, 0, len(modelStrings))
	for _, modelID := range modelStrings {
		modelRows = append(modelRows, record.TokenAllowedModel{
			TokenID: tokenID,
			ModelID: modelID,
		})
	}

	credentialIDs := sortedUniqueStrings(rules.AllowedCredentials)
	credentials := make([]record.TokenAllowedCredential, 0, len(credentialIDs))
	for _, credentialID := range credentialIDs {
		credentials = append(credentials, record.TokenAllowedCredential{
			TokenID:      tokenID,
			CredentialID: credentialID,
		})
	}

	grouped := map[string]*models.CredentialScope{}
	order := []string{}
	for _, scope := range rules.CredentialScopes {
		groupedScope, exists := grouped[scope.ProviderID]
		if !exists {
			groupedScope = &models.CredentialScope{ProviderID: scope.ProviderID}
			grouped[scope.ProviderID] = groupedScope
			order = append(order, scope.ProviderID)
		}
		if scope.All {
			groupedScope.All = true
		}
		groupedScope.CredentialIDs = append(groupedScope.CredentialIDs, scope.CredentialIDs...)
	}
	sort.Strings(order)

	scopes := make([]record.TokenCredentialScope, 0, len(order))
	scopeCredentials := []record.TokenScopeCredential{}
	for _, providerID := range order {
		groupedScope := grouped[providerID]
		if providerID == "" {
			return nil, nil, nil, nil, nil, fmt.Errorf("credential scope without provider: %w", errors.ErrInvalidInput)
		}
		scopeID, err := util.GenerateID()
		if err != nil {
			return nil, nil, nil, nil, nil, fmt.Errorf("generate scope identifier: %w", err)
		}
		scopes = append(scopes, record.TokenCredentialScope{
			ID:             scopeID,
			TokenID:        tokenID,
			ProviderID:     providerID,
			AllCredentials: groupedScope.All,
		})
		for _, credentialID := range sortedUniqueStrings(groupedScope.CredentialIDs) {
			scopeCredentials = append(scopeCredentials, record.TokenScopeCredential{
				ScopeID:      scopeID,
				CredentialID: credentialID,
			})
		}
	}

	return providers, modelRows, credentials, scopes, scopeCredentials, nil
}

// tokenRulesFromJoinRows rebuilds TokenRules from the base row flags and the
// join rows. Every list sorts so reloads compare equal across roundtrips.
func tokenRulesFromJoinRows(base record.RouterToken, providers []record.TokenAllowedProvider, modelRows []record.TokenAllowedModel, credentials []record.TokenAllowedCredential, scopes []record.TokenCredentialScope, scopeCredentials []record.TokenScopeCredential) models.TokenRules {
	rules := models.TokenRules{
		AllowAllProviders:   base.AllowAllProviders,
		AllowAllModels:      base.AllowAllModels,
		AllowAllCredentials: base.AllowAllCredentials,
	}

	providerIDs := make([]string, 0, len(providers))
	for _, provider := range providers {
		providerIDs = append(providerIDs, provider.ProviderID)
	}
	rules.AllowedProviders = sortedUniqueStrings(providerIDs)

	modelIDs := make([]string, 0, len(modelRows))
	for _, modelRow := range modelRows {
		modelIDs = append(modelIDs, modelRow.ModelID)
	}
	modelStrings := sortedUniqueStrings(modelIDs)
	rules.AllowedModels = make([]models.ModelId, 0, len(modelStrings))
	for _, modelID := range modelStrings {
		rules.AllowedModels = append(rules.AllowedModels, models.ModelId(modelID))
	}

	credentialIDs := make([]string, 0, len(credentials))
	for _, credential := range credentials {
		credentialIDs = append(credentialIDs, credential.CredentialID)
	}
	rules.AllowedCredentials = sortedUniqueStrings(credentialIDs)

	byScope := map[string][]string{}
	for _, scopeCredential := range scopeCredentials {
		byScope[scopeCredential.ScopeID] = append(byScope[scopeCredential.ScopeID], scopeCredential.CredentialID)
	}
	sortedScopes := append([]record.TokenCredentialScope(nil), scopes...)
	sort.Slice(sortedScopes, func(first, second int) bool {
		return sortedScopes[first].ProviderID < sortedScopes[second].ProviderID
	})
	for _, scope := range sortedScopes {
		rules.CredentialScopes = append(rules.CredentialScopes, models.CredentialScope{
			ProviderID:    scope.ProviderID,
			All:           scope.AllCredentials,
			CredentialIDs: sortedUniqueStrings(byScope[scope.ID]),
		})
	}

	return rules
}

// sortedUniqueStrings deduplicates and sorts values. Empty results read as
// nil so absent allow-lists stay absent instead of empty-but-present.
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

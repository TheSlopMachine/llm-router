// Package token implements the Internal Token Service.
package token

import (
	"context"
	"fmt"

	stdErrors "errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// GormService manages router tokens on the SQLite handle. The bbolt Service
// stays live until cutover; this type owns the GORM path with explicit
// converters between wire types and join rows. Traditional GORM use: none;
// every statement runs through gorm.G with typed clauses.
type GormService struct {
	database *db.Database
}

// NewGorm constructs a GORM-backed token service on the SQLite handle.
func NewGorm(database *db.Database) *GormService {
	return &GormService{database: database}
}

// Create mints a token and persists its base row plus join rows in one
// transaction. The raw secret returns once and never persists.
func (s *GormService) Create(ctx context.Context, options CreateOptions) (*models.RouterToken, error) {
	raw, err := util.GenerateToken()
	if err != nil {
		return nil, fmt.Errorf("generate token secret: %w", err)
	}
	identifier, err := util.GenerateID()
	if err != nil {
		return nil, fmt.Errorf("generate token identifier: %w", err)
	}
	token := &models.RouterToken{
		ID:        identifier,
		Name:      options.Name,
		Token:     raw,
		TokenHash: util.HashSecret(raw),
		Rules:     options.Rules,
		CreatedAt: util.Now(),
	}
	base := tokenRecordFromModel(token)
	providers, modelRows, credentials, scopes, scopeCredentials, err := tokenRulesToJoinRows(token.ID, token.Rules)
	if err != nil {
		return nil, err
	}
	if err := s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		if err := gorm.G[record.RouterToken](transaction).Create(ctx, &base); err != nil {
			return db.Translate(err)
		}
		return writeTokenJoinRows(ctx, transaction, providers, modelRows, credentials, scopes, scopeCredentials)
	}); err != nil {
		return nil, err
	}
	return token, nil
}

// Validate resolves the raw bearer value through the unique token hash
// index and rebuilds rules from the join tables. Unknown secrets deny with
// unauthorized, never not-found.
func (s *GormService) Validate(ctx context.Context, raw string) (*models.RouterToken, error) {
	hash := util.HashSecret(raw)
	var result *models.RouterToken
	if err := s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		base, err := gorm.G[record.RouterToken](transaction, clause.Eq{Column: "token_hash", Value: hash}).First(ctx)
		if err != nil {
			translated := db.Translate(err)
			if stdErrors.Is(translated, apierrors.ErrNotFound) {
				return apierrors.ErrUnauthorized
			}
			return translated
		}
		rules, err := loadTokenRules(ctx, transaction, base.ID)
		if err != nil {
			return err
		}
		result = tokenModelFromRecord(base, rules)
		return nil
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// Get returns one token with rules rebuilt from the join tables.
func (s *GormService) Get(ctx context.Context, identifier string) (*models.RouterToken, error) {
	var result *models.RouterToken
	if err := s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		base, err := gorm.G[record.RouterToken](transaction, clause.Eq{Column: "id", Value: identifier}).First(ctx)
		if err != nil {
			return db.Translate(err)
		}
		rules, err := loadTokenRules(ctx, transaction, base.ID)
		if err != nil {
			return err
		}
		result = tokenModelFromRecord(base, rules)
		return nil
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// List returns every token ordered by identifier with rules rebuilt per row.
func (s *GormService) List(ctx context.Context) ([]*models.RouterToken, error) {
	var result []*models.RouterToken
	if err := s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		bases, err := gorm.G[record.RouterToken](transaction, clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "id"}}}}).Find(ctx)
		if err != nil {
			return db.Translate(err)
		}
		tokens := make([]*models.RouterToken, 0, len(bases))
		for _, base := range bases {
			rules, err := loadTokenRules(ctx, transaction, base.ID)
			if err != nil {
				return err
			}
			tokens = append(tokens, tokenModelFromRecord(base, rules))
		}
		result = tokens
		return nil
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// Delete removes one token. Join rows cascade from the base delete. A zero
// row delete reports not-found through the WithResult tracker.
func (s *GormService) Delete(ctx context.Context, identifier string) error {
	return s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		tracker := gorm.WithResult()
		affected, err := gorm.G[record.RouterToken](transaction, clause.Eq{Column: "id", Value: identifier}, tracker).Delete(ctx)
		if err != nil {
			return db.Translate(err)
		}
		if affected == 0 || tracker.RowsAffected == 0 {
			return fmt.Errorf("token %q: %w", identifier, apierrors.ErrNotFound)
		}
		return nil
	})
}

// UpdateRules replaces allow flags and every join row in one transaction.
func (s *GormService) UpdateRules(ctx context.Context, identifier string, rules models.TokenRules) error {
	providers, modelRows, credentials, scopes, scopeCredentials, err := tokenRulesToJoinRows(identifier, rules)
	if err != nil {
		return err
	}
	return s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		base, err := gorm.G[record.RouterToken](transaction, clause.Eq{Column: "id", Value: identifier}).First(ctx)
		if err != nil {
			return db.Translate(err)
		}
		base.AllowAllProviders = rules.AllowAllProviders
		base.AllowAllModels = rules.AllowAllModels
		base.AllowAllCredentials = rules.AllowAllCredentials
		rows := []record.RouterToken{base}
		if err := gorm.G[record.RouterToken](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).CreateInBatches(ctx, &rows, 500); err != nil {
			return db.Translate(err)
		}
		if err := deleteTokenJoinRows(ctx, transaction, identifier); err != nil {
			return err
		}
		return writeTokenJoinRows(ctx, transaction, providers, modelRows, credentials, scopes, scopeCredentials)
	})
}

// Regenerate replaces the secret while keeping identifier, name, and rules.
// The single-column Update keeps the hash write minimal.
func (s *GormService) Regenerate(ctx context.Context, identifier string) (*models.RouterToken, error) {
	raw, err := util.GenerateToken()
	if err != nil {
		return nil, fmt.Errorf("generate token secret: %w", err)
	}
	freshHash := util.HashSecret(raw)
	var result *models.RouterToken
	if err := s.database.Handle.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		base, err := gorm.G[record.RouterToken](transaction, clause.Eq{Column: "id", Value: identifier}).First(ctx)
		if err != nil {
			return db.Translate(err)
		}
		tracker := gorm.WithResult()
		affected, err := gorm.G[record.RouterToken](transaction, clause.Eq{Column: "id", Value: identifier}, tracker).Update(ctx, "token_hash", freshHash)
		if err != nil {
			return db.Translate(err)
		}
		if affected == 0 || tracker.RowsAffected == 0 {
			return fmt.Errorf("token %q: %w", identifier, apierrors.ErrNotFound)
		}
		base.TokenHash = freshHash
		rules, err := loadTokenRules(ctx, transaction, base.ID)
		if err != nil {
			return err
		}
		result = tokenModelFromRecord(base, rules)
		result.Token = raw
		return nil
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// loadTokenRules rebuilds rules for one token from the five join tables on
// the transaction handle. Finds read empty as empty, never as not-found.
func loadTokenRules(ctx context.Context, transaction *gorm.DB, tokenID string) (models.TokenRules, error) {
	providers, err := gorm.G[record.TokenAllowedProvider](transaction, clause.Eq{Column: "token_id", Value: tokenID}).Find(ctx)
	if err != nil {
		return models.TokenRules{}, db.Translate(err)
	}
	modelRows, err := gorm.G[record.TokenAllowedModel](transaction, clause.Eq{Column: "token_id", Value: tokenID}).Find(ctx)
	if err != nil {
		return models.TokenRules{}, db.Translate(err)
	}
	credentials, err := gorm.G[record.TokenAllowedCredential](transaction, clause.Eq{Column: "token_id", Value: tokenID}).Find(ctx)
	if err != nil {
		return models.TokenRules{}, db.Translate(err)
	}
	scopes, err := gorm.G[record.TokenCredentialScope](transaction, clause.Eq{Column: "token_id", Value: tokenID}).Find(ctx)
	if err != nil {
		return models.TokenRules{}, db.Translate(err)
	}
	scopeCredentials := []record.TokenScopeCredential{}
	for _, scope := range scopes {
		pinned, err := gorm.G[record.TokenScopeCredential](transaction, clause.Eq{Column: "scope_id", Value: scope.ID}).Find(ctx)
		if err != nil {
			return models.TokenRules{}, db.Translate(err)
		}
		scopeCredentials = append(scopeCredentials, pinned...)
	}
	base, err := gorm.G[record.RouterToken](transaction, clause.Eq{Column: "id", Value: tokenID}).First(ctx)
	if err != nil {
		return models.TokenRules{}, db.Translate(err)
	}
	return tokenRulesFromJoinRows(base, providers, modelRows, credentials, scopes, scopeCredentials), nil
}

// writeTokenJoinRows upserts every join slice. Empty slices skip the call.
// Primary key targets keep secondary unique violations as duplicate errors.
func writeTokenJoinRows(ctx context.Context, transaction *gorm.DB, providers []record.TokenAllowedProvider, modelRows []record.TokenAllowedModel, credentials []record.TokenAllowedCredential, scopes []record.TokenCredentialScope, scopeCredentials []record.TokenScopeCredential) error {
	if len(providers) > 0 {
		if err := gorm.G[record.TokenAllowedProvider](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "token_id"}, {Name: "provider_id"}}, UpdateAll: true}).CreateInBatches(ctx, &providers, 500); err != nil {
			return db.Translate(err)
		}
	}
	if len(modelRows) > 0 {
		if err := gorm.G[record.TokenAllowedModel](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "token_id"}, {Name: "model_id"}}, UpdateAll: true}).CreateInBatches(ctx, &modelRows, 500); err != nil {
			return db.Translate(err)
		}
	}
	if len(credentials) > 0 {
		if err := gorm.G[record.TokenAllowedCredential](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "token_id"}, {Name: "credential_id"}}, UpdateAll: true}).CreateInBatches(ctx, &credentials, 500); err != nil {
			return db.Translate(err)
		}
	}
	if len(scopes) > 0 {
		if err := gorm.G[record.TokenCredentialScope](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).CreateInBatches(ctx, &scopes, 500); err != nil {
			return db.Translate(err)
		}
	}
	if len(scopeCredentials) > 0 {
		if err := gorm.G[record.TokenScopeCredential](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "scope_id"}, {Name: "credential_id"}}, UpdateAll: true}).CreateInBatches(ctx, &scopeCredentials, 500); err != nil {
			return db.Translate(err)
		}
	}
	return nil
}

// deleteTokenJoinRows removes the per-token join rows. Scope pins cascade
// from the scope delete; every delete carries its token condition.
func deleteTokenJoinRows(ctx context.Context, transaction *gorm.DB, tokenID string) error {
	if _, err := gorm.G[record.TokenAllowedProvider](transaction, clause.Eq{Column: "token_id", Value: tokenID}).Delete(ctx); err != nil {
		return db.Translate(err)
	}
	if _, err := gorm.G[record.TokenAllowedModel](transaction, clause.Eq{Column: "token_id", Value: tokenID}).Delete(ctx); err != nil {
		return db.Translate(err)
	}
	if _, err := gorm.G[record.TokenAllowedCredential](transaction, clause.Eq{Column: "token_id", Value: tokenID}).Delete(ctx); err != nil {
		return db.Translate(err)
	}
	if _, err := gorm.G[record.TokenCredentialScope](transaction, clause.Eq{Column: "token_id", Value: tokenID}).Delete(ctx); err != nil {
		return db.Translate(err)
	}
	return nil
}

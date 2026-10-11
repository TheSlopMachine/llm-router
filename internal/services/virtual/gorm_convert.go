package virtual

import (
	"strings"

	"github.com/TheSlopMachine/llm-router/internal/db/record"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

// reservedAgentSlugs collide with dashboard routes.
var reservedAgentSlugs = map[string]bool{"new": true}

// agentSlug converts a name to a URL-safe slug, or "" when unusable.
func agentSlug(name string) string {
	return util.Slugify(name)
}

// virtualNameKey lowercases a virtual model name for the name_key UNIQUE
// column. Case-insensitive duplicates collide on this key, matching the
// previous EqualFold scan.
func virtualNameKey(name string) string {
	return strings.ToLower(name)
}

// virtualRecordFromModel maps the wire virtual model onto the base row plus
// position-ordered member rows. ManagedBy splits into the managed columns:
// both halves persist together, an empty marker persists as NULLs. Member
// model IDs split at the first slash into provider and model halves, so a
// member referencing another virtual model lands on the virtual provider
// row. Timestamps normalize to UTC. Parse failures return an error; the
// caller runs no I/O here.
func virtualRecordFromModel(virtualModel *models.VirtualModel, identifier string) (record.VirtualModel, []record.VirtualModelMember, error) {
	row := record.VirtualModel{
		ID:          identifier,
		Name:        virtualModel.Name,
		NameKey:     virtualNameKey(virtualModel.Name),
		Description: virtualModel.Description,
		Instruction: virtualModel.Instruction,
		Disabled:    virtualModel.Disabled,
		Version:     virtualModel.Version,
		CreatedAt:   virtualModel.CreatedAt.UTC(),
		UpdatedAt:   virtualModel.UpdatedAt.UTC(),
	}
	if virtualModel.ManagedBy != "" {
		providerID, slug, err := ParseMarker(virtualModel.ManagedBy)
		if err != nil {
			return record.VirtualModel{}, nil, err
		}
		row.ManagedProviderID = &providerID
		row.ManagedEndpointSlug = &slug
	}
	members := make([]record.VirtualModelMember, 0, len(virtualModel.Models))
	for position, entry := range virtualModel.Models {
		providerID, modelName, err := entry.ModelID.Parse()
		if err != nil {
			return record.VirtualModel{}, nil, err
		}
		members = append(members, record.VirtualModelMember{
			VirtualModelID: identifier,
			Position:       position,
			ProviderID:     providerID,
			ModelName:      modelName,
		})
	}
	return row, members, nil
}

// virtualModelFromRecords rebuilds the wire virtual model from the base row
// plus its member rows. Members must arrive ordered by position; the caller
// query owns that ordering. A marker recomposes only when both managed
// halves read present, otherwise the model reads manual. Timestamps return
// in UTC.
func virtualModelFromRecords(row record.VirtualModel, members []record.VirtualModelMember) *models.VirtualModel {
	virtualModel := &models.VirtualModel{
		ID:          row.ID,
		Name:        row.Name,
		Description: row.Description,
		Instruction: row.Instruction,
		Disabled:    row.Disabled,
		Version:     row.Version,
		CreatedAt:   row.CreatedAt.UTC(),
		UpdatedAt:   row.UpdatedAt.UTC(),
	}
	if row.ManagedProviderID != nil && row.ManagedEndpointSlug != nil {
		virtualModel.ManagedBy = ProviderMarker(*row.ManagedProviderID, *row.ManagedEndpointSlug)
	}
	entries := make([]models.VirtualModelEntry, 0, len(members))
	for _, member := range members {
		entries = append(entries, models.VirtualModelEntry{ModelID: models.ModelId(member.ProviderID + "/" + member.ModelName)})
	}
	virtualModel.Models = entries
	return virtualModel
}

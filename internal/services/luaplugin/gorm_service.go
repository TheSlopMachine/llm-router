package luaplugin

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	stdErrors "errors"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/util"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// maxPluginHistoryEntries caps rollback history at ten full-source snapshots
// per plugin, matching the previous History discipline.
const maxPluginHistoryEntries = 10

// New constructs a SQLite-backed plugin Service and loads the in-memory
// registry from the plugin rows. Construction reads storage once so
// handler lookups never touch the database.
func New(sqliteDatabase *db.Database, logger *slog.Logger) (*Service, error) {
	if sqliteDatabase == nil || sqliteDatabase.Handle == nil {
		return nil, fmt.Errorf("luaplugin sqlite handle is not configured: %w", apierrors.ErrInvalidInput)
	}
	service := &Service{
		database: sqliteDatabase.Handle,
		logger:   logger,
		registry: map[string]*PluginRecord{},
		logs:     map[string][]LogEntry{},
		crashes:  map[string][]CrashEntry{},
	}
	if err := service.rebuild(context.Background()); err != nil {
		return nil, err
	}
	return service, nil
}

// requireHandle fails closed when the SQLite handle is missing.
func (service *Service) requireHandle() error {
	if service == nil || service.database == nil {
		return fmt.Errorf("luaplugin sqlite handle is not configured: %w", apierrors.ErrInvalidInput)
	}
	return nil
}

// orderPluginsByIdentifier sorts plugin rows by primary key.
func orderPluginsByIdentifier() clause.OrderBy {
	return clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "id"}}}}
}

// orderAllowHostsByHost sorts allow host rows by host for determinism.
func orderAllowHostsByHost() clause.OrderBy {
	return clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "host"}}}}
}

// orderVersionsByCreation sorts version rows oldest first with identifier
// tiebreak so history rebuilds in insertion order.
func orderVersionsByCreation() clause.OrderBy {
	return clause.OrderBy{Columns: []clause.OrderByColumn{
		{Column: clause.Column{Name: "created_at"}},
		{Column: clause.Column{Name: "id"}},
	}}
}

// InstallSQLite validates plugin source outside the transaction and persists
// the plugin row plus allow host and version child rows in one transaction.
// Lua execution stays outside the transaction; the transaction uses its
// handle only.
func (service *Service) InstallSQLite(ctx context.Context, source []byte, origin PluginOrigin) (*PluginRecord, error) {
	if len(source) == 0 {
		return nil, fmt.Errorf("plugin source is empty")
	}
	if len(source) > maxPluginSource {
		return nil, fmt.Errorf("plugin source exceeds %d bytes", maxPluginSource)
	}
	manifest, err := ParseManifest(source)
	if err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	if err := CheckPluginAPI(manifest); err != nil {
		return nil, err
	}
	identifier, err := BuildID(origin, manifest)
	if err != nil {
		return nil, err
	}
	dryOutput, err := service.dryRun(identifier, source, manifest)
	if err != nil {
		return nil, err
	}
	typeKeys := dryOutput.keys
	if err := service.requireHandle(); err != nil {
		return nil, err
	}
	var installed *PluginRecord
	var previousKeys []string
	var previousRecord *PluginRecord
	if err := service.database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		if err := checkTypeKeyConflictsInTransaction(ctx, transaction, identifier, typeKeys); err != nil {
			return err
		}
		existingRow, exists, err := loadPluginRowInTransaction(ctx, transaction, identifier)
		if err != nil {
			return err
		}
		now := util.Now()
		if !exists {
			pluginRecord := &PluginRecord{
				ID:                  identifier,
				DisplayName:         manifest.Plugin,
				Author:              manifest.Author,
				Version:             manifest.Version,
				PluginAPI:           manifest.PluginAPI,
				Description:         manifest.Description,
				License:             manifest.License,
				AllowHosts:          append([]string(nil), manifest.AllowHosts...),
				Unsafe:              manifest.Unsafe,
				ProxySourceKeys:     append([]string(nil), dryOutput.sourceKeys...),
				ModelSpecifications: dryOutput.specifications,
				HealthCooldown:      dryOutput.cooldowns,
				Schemas:             dryOutput.schemas,
				Locales:             dryOutput.locales,
				Jobs:                dryOutput.jobs,
				TypeKeys:            append([]string(nil), typeKeys...),
				Handlers:            dryOutput.handlers,
				Icons:               dryOutput.icons,
				Source:              append([]byte(nil), source...),
				Origin:              origin,
				InstalledAt:         now,
				UpdatedAt:           now,
			}
			if err := writePluginRecordInTransaction(ctx, transaction, pluginRecord, now, now); err != nil {
				return err
			}
			installed = pluginRecord
			return nil
		}
		existingRecord, err := func() (*PluginRecord, error) {
			hostRows, hostErr := gorm.G[record.PluginAllowHost](transaction, clause.Eq{Column: "plugin_id", Value: identifier}, orderAllowHostsByHost()).Find(ctx)
			if hostErr != nil {
				return nil, db.Translate(hostErr)
			}
			versionRows, versionErr := gorm.G[record.PluginVersion](transaction, clause.Eq{Column: "plugin_id", Value: identifier}, orderVersionsByCreation()).Find(ctx)
			if versionErr != nil {
				return nil, db.Translate(versionErr)
			}
			return pluginRecordFromRows(existingRow, hostRows, versionRows)
		}()
		if err != nil {
			return err
		}
		previousSnapshot := snapshotFromRecord(existingRecord)
		previousKeys = append([]string(nil), existingRecord.TypeKeys...)
		previousRecord = existingRecord
		versionIdentifier, err := util.GenerateID()
		if err != nil {
			return fmt.Errorf("generate version identifier: %w", err)
		}
		versionRow := versionRowFromSnapshot(identifier, previousSnapshot, versionIdentifier, now)
		if err := writeVersionRowInTransaction(ctx, transaction, versionRow); err != nil {
			return err
		}
		if err := pruneVersionHistoryInTransaction(ctx, transaction, identifier); err != nil {
			return err
		}
		history := append(existingRecord.History, previousSnapshot)
		if len(history) > maxPluginHistoryEntries {
			history = history[len(history)-maxPluginHistoryEntries:]
		}
		updated := &PluginRecord{
			ID:                  identifier,
			DisplayName:         manifest.Plugin,
			Author:              manifest.Author,
			Version:             manifest.Version,
			PluginAPI:           manifest.PluginAPI,
			Description:         manifest.Description,
			License:             manifest.License,
			AllowHosts:          append([]string(nil), manifest.AllowHosts...),
			Unsafe:              manifest.Unsafe,
			ProxySourceKeys:     append([]string(nil), dryOutput.sourceKeys...),
			ModelSpecifications: dryOutput.specifications,
			HealthCooldown:      dryOutput.cooldowns,
			Schemas:             dryOutput.schemas,
			Locales:             dryOutput.locales,
			Jobs:                dryOutput.jobs,
			TypeKeys:            append([]string(nil), typeKeys...),
			Handlers:            dryOutput.handlers,
			Icons:               dryOutput.icons,
			Source:              append([]byte(nil), source...),
			History:             history,
			Origin:              origin,
			InstalledAt:         existingRecord.InstalledAt,
			UpdatedAt:           now,
		}
		if err := writePluginRecordInTransaction(ctx, transaction, updated, existingRecord.InstalledAt, now); err != nil {
			return err
		}
		installed = updated
		return nil
	}); err != nil {
		return nil, err
	}
	if err := service.rebuild(ctx); err != nil {
		return nil, err
	}
	service.notify(unionKeys(previousKeys, installed.TypeKeys)...)
	if previousRecord != nil {
		service.launchLifecycleHook(previousRecord, string(HandlerDeinitialize), DeinitializeReasonUpdate)
		service.launchLifecycleHook(installed, string(HandlerInitialize), InitializeReasonUpdate)
	} else {
		service.launchLifecycleHook(installed, string(HandlerInitialize), InitializeReasonInstall)
	}
	return installed, nil
}

// GetSQLite returns one plugin record with hosts and history assembled from
// the child tables in one transaction. A miss reports the not-found sentinel.
func (service *Service) GetSQLite(ctx context.Context, pluginID string) (*PluginRecord, error) {
	if err := service.requireHandle(); err != nil {
		return nil, err
	}
	var result *PluginRecord
	if err := service.database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		baseRow, err := gorm.G[record.Plugin](transaction, clause.Eq{Column: "id", Value: pluginID}).First(ctx)
		if err != nil {
			return db.Translate(err)
		}
		hostRows, err := gorm.G[record.PluginAllowHost](transaction, clause.Eq{Column: "plugin_id", Value: pluginID}, orderAllowHostsByHost()).Find(ctx)
		if err != nil {
			return db.Translate(err)
		}
		versionRows, err := gorm.G[record.PluginVersion](transaction, clause.Eq{Column: "plugin_id", Value: pluginID}, orderVersionsByCreation()).Find(ctx)
		if err != nil {
			return db.Translate(err)
		}
		assembled, err := pluginRecordFromRows(baseRow, hostRows, versionRows)
		if err != nil {
			return err
		}
		result = assembled
		return nil
	}); err != nil {
		if stdErrors.Is(err, apierrors.ErrNotFound) {
			return nil, fmt.Errorf("plugin %q: %w", pluginID, apierrors.ErrNotFound)
		}
		return nil, err
	}
	return result, nil
}

// ListSQLite returns every plugin record ordered by identifier with hosts
// and history assembled per row in one transaction.
func (service *Service) ListSQLite(ctx context.Context) ([]*PluginRecord, error) {
	if err := service.requireHandle(); err != nil {
		return nil, err
	}
	var result []*PluginRecord
	if err := service.database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		pluginRows, err := gorm.G[record.Plugin](transaction, orderPluginsByIdentifier()).Find(ctx)
		if err != nil {
			return db.Translate(err)
		}
		hostRows, err := gorm.G[record.PluginAllowHost](transaction, orderAllowHostsByHost()).Find(ctx)
		if err != nil {
			return db.Translate(err)
		}
		versionRows, err := gorm.G[record.PluginVersion](transaction, orderVersionsByCreation()).Find(ctx)
		if err != nil {
			return db.Translate(err)
		}
		hostsByPlugin := map[string][]record.PluginAllowHost{}
		for _, hostRow := range hostRows {
			hostsByPlugin[hostRow.PluginID] = append(hostsByPlugin[hostRow.PluginID], hostRow)
		}
		versionsByPlugin := map[string][]record.PluginVersion{}
		for _, versionRow := range versionRows {
			versionsByPlugin[versionRow.PluginID] = append(versionsByPlugin[versionRow.PluginID], versionRow)
		}
		assembled := make([]*PluginRecord, 0, len(pluginRows))
		for _, pluginRow := range pluginRows {
			pluginRecord, err := pluginRecordFromRows(pluginRow, hostsByPlugin[pluginRow.ID], versionsByPlugin[pluginRow.ID])
			if err != nil {
				return err
			}
			assembled = append(assembled, pluginRecord)
		}
		result = assembled
		return nil
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// DeleteSQLite removes one plugin row. Allow host, version and storage rows
// cascade from the base delete. A zero-row delete reports the not-found
// sentinel through the WithResult tracker. The registry rebuilds and
// notifies the removed type keys.
func (service *Service) DeleteSQLite(ctx context.Context, pluginID string) error {
	if err := service.requireHandle(); err != nil {
		return err
	}
	existing, err := service.GetSQLite(ctx, pluginID)
	if err != nil {
		return err
	}
	// The hook invocation precedes the delete; execution overlaps it
	// detached and the delete proceeds regardless of the outcome.
	service.launchLifecycleHook(existing, string(HandlerDeinitialize), DeinitializeReasonDelete)
	if err := service.database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		tracker := gorm.WithResult()
		affected, err := gorm.G[record.Plugin](transaction, clause.Eq{Column: "id", Value: pluginID}, tracker).Delete(ctx)
		if err != nil {
			return db.Translate(err)
		}
		if affected == 0 || tracker.RowsAffected == 0 {
			return fmt.Errorf("plugin %q: %w", pluginID, apierrors.ErrNotFound)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := service.rebuild(ctx); err != nil {
		return err
	}
	service.notify(existing.TypeKeys...)
	return nil
}

// RollbackSQLite restores the most recent previous version. Reads for the
// Lua fallback happen outside the transaction on the base handle; the single
// transaction holds the read-modify-write for the plugin row, host
// replacement, version delete and version insert. The rolled-back-from
// version returns to history so rollback toggles forward again, matching
// the previous discipline.
func (service *Service) RollbackSQLite(ctx context.Context, pluginID string) (*PluginRecord, error) {
	if err := service.requireHandle(); err != nil {
		return nil, err
	}
	pluginRow, err := gorm.G[record.Plugin](service.database, clause.Eq{Column: "id", Value: pluginID}).First(ctx)
	if err != nil {
		translated := db.Translate(err)
		if stdErrors.Is(translated, apierrors.ErrNotFound) {
			return nil, fmt.Errorf("plugin %q: %w", pluginID, apierrors.ErrNotFound)
		}
		return nil, translated
	}
	hostRows, err := gorm.G[record.PluginAllowHost](service.database, clause.Eq{Column: "plugin_id", Value: pluginID}, orderAllowHostsByHost()).Find(ctx)
	if err != nil {
		return nil, db.Translate(err)
	}
	versionRows, err := gorm.G[record.PluginVersion](service.database, clause.Eq{Column: "plugin_id", Value: pluginID}, orderVersionsByCreation()).Find(ctx)
	if err != nil {
		return nil, db.Translate(err)
	}
	currentRecord, err := pluginRecordFromRows(pluginRow, hostRows, versionRows)
	if err != nil {
		return nil, err
	}
	if len(currentRecord.History) == 0 {
		return nil, fmt.Errorf("plugin %q has no previous version", pluginID)
	}
	previousSnapshot := currentRecord.History[len(currentRecord.History)-1]
	remainingHistory := append([]PluginVersionSnapshot(nil), currentRecord.History[:len(currentRecord.History)-1]...)
	manifest, err := ParseManifest(previousSnapshot.Source)
	if err != nil {
		return nil, fmt.Errorf("previous version manifest: %w", err)
	}
	handlers := previousSnapshot.Handlers
	icons := previousSnapshot.Icons
	specifications := previousSnapshot.ModelSpecifications
	cooldowns := previousSnapshot.HealthCooldown
	sourceKeys := previousSnapshot.ProxySourceKeys
	schemas := previousSnapshot.Schemas
	locales := previousSnapshot.Locales
	jobs := previousSnapshot.Jobs
	if handlers == nil {
		dryOutput, dryErr := service.dryRun(pluginID, previousSnapshot.Source, manifest)
		if dryErr != nil {
			return nil, fmt.Errorf("previous version dry-run: %w", dryErr)
		}
		handlers = dryOutput.handlers
		icons = dryOutput.icons
		specifications = dryOutput.specifications
		cooldowns = dryOutput.cooldowns
		sourceKeys = dryOutput.sourceKeys
		schemas = dryOutput.schemas
		locales = dryOutput.locales
		jobs = dryOutput.jobs
	}
	rolledSnapshot := snapshotFromRecord(currentRecord)
	remainingHistory = append(remainingHistory, rolledSnapshot)
	if len(remainingHistory) > maxPluginHistoryEntries {
		remainingHistory = remainingHistory[len(remainingHistory)-maxPluginHistoryEntries:]
	}
	rolled := &PluginRecord{
		ID:                  currentRecord.ID,
		DisplayName:         manifest.Plugin,
		Author:              manifest.Author,
		Version:             previousSnapshot.Version,
		PluginAPI:           manifest.PluginAPI,
		Description:         manifest.Description,
		License:             manifest.License,
		AllowHosts:          append([]string(nil), manifest.AllowHosts...),
		Unsafe:              manifest.Unsafe,
		ProxySourceKeys:     append([]string(nil), sourceKeys...),
		ModelSpecifications: specifications,
		HealthCooldown:      cooldowns,
		Schemas:             schemas,
		Locales:             locales,
		Jobs:                jobs,
		TypeKeys:            append([]string(nil), previousSnapshot.TypeKeys...),
		Handlers:            handlers,
		Icons:               icons,
		Source:              append([]byte(nil), previousSnapshot.Source...),
		History:             remainingHistory,
		Origin:              currentRecord.Origin,
		InstalledAt:         currentRecord.InstalledAt,
		UpdatedAt:           util.Now(),
	}
	rolledBackIdentifier, err := util.GenerateID()
	if err != nil {
		return nil, fmt.Errorf("generate version identifier: %w", err)
	}
	rolledRow := versionRowFromSnapshot(pluginID, rolledSnapshot, rolledBackIdentifier, util.Now())
	if err := service.database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		versionRows, err := gorm.G[record.PluginVersion](transaction, clause.Eq{Column: "plugin_id", Value: pluginID}, orderVersionsByCreation()).Find(ctx)
		if err != nil {
			return db.Translate(err)
		}
		if len(versionRows) == 0 {
			return fmt.Errorf("plugin %q has no previous version", pluginID)
		}
		ordered := append([]record.PluginVersion(nil), versionRows...)
		sort.Slice(ordered, func(first int, second int) bool {
			if ordered[first].CreatedAt.Equal(ordered[second].CreatedAt) {
				return ordered[first].ID < ordered[second].ID
			}
			return ordered[first].CreatedAt.Before(ordered[second].CreatedAt)
		})
		latest := ordered[len(ordered)-1]
		tracker := gorm.WithResult()
		affected, err := gorm.G[record.PluginVersion](transaction, clause.Eq{Column: "id", Value: latest.ID}, tracker).Delete(ctx)
		if err != nil {
			return db.Translate(err)
		}
		if affected == 0 || tracker.RowsAffected == 0 {
			return fmt.Errorf("plugin %q has no previous version", pluginID)
		}
		if err := writeVersionRowInTransaction(ctx, transaction, rolledRow); err != nil {
			return err
		}
		if err := pruneVersionHistoryInTransaction(ctx, transaction, pluginID); err != nil {
			return err
		}
		return writePluginRecordInTransaction(ctx, transaction, rolled, rolled.InstalledAt, rolled.UpdatedAt)
	}); err != nil {
		return nil, err
	}
	if err := service.rebuild(ctx); err != nil {
		return nil, err
	}
	service.notify(rolled.TypeKeys...)
	service.launchLifecycleHook(currentRecord, string(HandlerDeinitialize), DeinitializeReasonUpdate)
	service.launchLifecycleHook(rolled, string(HandlerInitialize), InitializeReasonUpdate)
	return rolled, nil
}

// checkTypeKeyConflictsInTransaction rejects type keys already served by
// another plugin row on the transaction handle. Updating the same plugin
// identifier is allowed; claiming a foreign key is not.
func checkTypeKeyConflictsInTransaction(ctx context.Context, transaction *gorm.DB, identifier string, typeKeys []string) error {
	claimed := make(map[string]string, len(typeKeys))
	for _, typeKey := range typeKeys {
		claimed[typeKey] = identifier
	}
	pluginRows, err := gorm.G[record.Plugin](transaction).Find(ctx)
	if err != nil {
		return db.Translate(err)
	}
	for _, pluginRow := range pluginRows {
		if pluginRow.ID == identifier {
			continue
		}
		var storedKeys []string
		if err := decodeDeclarationsField(pluginRow.Declarations, "type_keys", &storedKeys); err != nil {
			return err
		}
		for _, typeKey := range storedKeys {
			if owner, ok := claimed[typeKey]; ok && owner == identifier {
				return fmt.Errorf("%w: type key %q claimed by plugin %q", ErrTypeKeyConflict, typeKey, pluginRow.ID)
			}
		}
	}
	return nil
}

// loadPluginRowInTransaction reads one plugin row on the transaction handle.
// A miss returns exists false with a translated nil error, never a
// not-found error, so callers branch on existence.
func loadPluginRowInTransaction(ctx context.Context, transaction *gorm.DB, pluginID string) (record.Plugin, bool, error) {
	pluginRow, err := gorm.G[record.Plugin](transaction, clause.Eq{Column: "id", Value: pluginID}).First(ctx)
	if err != nil {
		translated := db.Translate(err)
		if stdErrors.Is(translated, apierrors.ErrNotFound) {
			return record.Plugin{}, false, nil
		}
		return record.Plugin{}, false, translated
	}
	return pluginRow, true, nil
}

// writePluginRecordInTransaction upserts the base plugin row and replaces
// its allow host rows on the transaction handle. Host replacement deletes
// by plugin condition then batch-inserts the current list. The conflict
// target is the primary key so secondary violations surface as duplicates.
func writePluginRecordInTransaction(ctx context.Context, transaction *gorm.DB, pluginRecord *PluginRecord, installedAt time.Time, updatedAt time.Time) error {
	pluginRow := pluginRowFromRecord(pluginRecord, installedAt, updatedAt)
	rows := []record.Plugin{pluginRow}
	if err := gorm.G[record.Plugin](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).CreateInBatches(ctx, &rows, 500); err != nil {
		return db.Translate(err)
	}
	if _, err := gorm.G[record.PluginAllowHost](transaction, clause.Eq{Column: "plugin_id", Value: pluginRecord.ID}).Delete(ctx); err != nil {
		return db.Translate(err)
	}
	hostRows := allowHostRowsFromRecord(pluginRecord)
	if len(hostRows) > 0 {
		if err := gorm.G[record.PluginAllowHost](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "plugin_id"}, {Name: "host"}}, UpdateAll: true}).CreateInBatches(ctx, &hostRows, 500); err != nil {
			return db.Translate(err)
		}
	}
	return nil
}

// writeVersionRowInTransaction inserts one history row on the transaction
// handle. The conflict target is the generated primary key.
func writeVersionRowInTransaction(ctx context.Context, transaction *gorm.DB, versionRow record.PluginVersion) error {
	rows := []record.PluginVersion{versionRow}
	if err := gorm.G[record.PluginVersion](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).CreateInBatches(ctx, &rows, 500); err != nil {
		return db.Translate(err)
	}
	return nil
}

// pruneVersionHistoryInTransaction keeps the ten most recent history rows
// for one plugin on the transaction handle and deletes the older excess by
// primary key condition.
func pruneVersionHistoryInTransaction(ctx context.Context, transaction *gorm.DB, pluginID string) error {
	versionRows, err := gorm.G[record.PluginVersion](transaction, clause.Eq{Column: "plugin_id", Value: pluginID}, orderVersionsByCreation()).Find(ctx)
	if err != nil {
		return db.Translate(err)
	}
	if len(versionRows) <= maxPluginHistoryEntries {
		return nil
	}
	excess := versionRows[:len(versionRows)-maxPluginHistoryEntries]
	for _, versionRow := range excess {
		if _, err := gorm.G[record.PluginVersion](transaction, clause.Eq{Column: "id", Value: versionRow.ID}).Delete(ctx); err != nil {
			return db.Translate(err)
		}
	}
	return nil
}

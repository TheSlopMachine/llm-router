package datamanagement

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/pluginrepo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/token"
	"github.com/TheSlopMachine/llm-router/internal/services/virtual"
)

// stubProviderBackend is an in-memory GormProviderBackend with the real
// dedup discipline: qualified creates mint type:qualifier with numeric
// suffixes on collision.
type stubProviderBackend struct {
	mutex       sync.Mutex
	providers   map[string]*models.ProviderInstance
	updateError error
}

func newStubProviderBackend() *stubProviderBackend {
	return &stubProviderBackend{providers: map[string]*models.ProviderInstance{}}
}

func (s *stubProviderBackend) Get(_ context.Context, identifier string) (*models.ProviderInstance, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	instance, ok := s.providers[identifier]
	if !ok {
		return nil, apierrors.NewNotFoundError("provider", identifier)
	}
	return instance, nil
}

func (s *stubProviderBackend) List(_ context.Context) ([]*models.ProviderInstance, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	items := make([]*models.ProviderInstance, 0, len(s.providers))
	for _, instance := range s.providers {
		items = append(items, instance)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (s *stubProviderBackend) Create(_ context.Context, options provider.CreateOptions) (*models.ProviderInstance, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	base := options.TypeKey
	if options.Qualifier != "" {
		base = options.TypeKey + ":" + strings.ToLower(strings.ReplaceAll(options.Qualifier, " ", "-"))
	}
	identifier := base
	for counter := 2; ; counter++ {
		if _, taken := s.providers[identifier]; !taken {
			break
		}
		identifier = fmt.Sprintf("%s-%d", base, counter)
	}
	instance := &models.ProviderInstance{
		ID: identifier, Name: options.Name, TypeKey: options.TypeKey,
		Qualifier: options.Qualifier, Config: options.Config, IconURL: options.IconURL,
	}
	s.providers[identifier] = instance
	return instance, nil
}

func (s *stubProviderBackend) Update(_ context.Context, identifier string, options provider.UpdateOptions) (*models.ProviderInstance, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.updateError != nil {
		return nil, s.updateError
	}
	instance, ok := s.providers[identifier]
	if !ok {
		return nil, apierrors.NewNotFoundError("provider", identifier)
	}
	instance.Name = options.Name
	instance.Config = options.Config
	instance.IconURL = options.IconURL
	if options.Disabled != nil {
		instance.Disabled = *options.Disabled
	}
	return instance, nil
}

func (s *stubProviderBackend) Delete(_ context.Context, identifier string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if _, ok := s.providers[identifier]; !ok {
		return fmt.Errorf("provider %q: %w", identifier, apierrors.ErrNotFound)
	}
	delete(s.providers, identifier)
	return nil
}

// stubCredentialBackend is an in-memory GormCredentialBackend.
type stubCredentialBackend struct {
	mutex       sync.Mutex
	credentials map[string]*models.Credential
	counter     int
}

func newStubCredentialBackend() *stubCredentialBackend {
	return &stubCredentialBackend{credentials: map[string]*models.Credential{}}
}

func (s *stubCredentialBackend) ListAll(_ context.Context) ([]*models.Credential, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	items := make([]*models.Credential, 0, len(s.credentials))
	for _, credentialItem := range s.credentials {
		items = append(items, credentialItem)
	}
	return items, nil
}

func (s *stubCredentialBackend) ListByProvider(_ context.Context, providerID string) ([]*models.Credential, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	var items []*models.Credential
	for _, credentialItem := range s.credentials {
		if credentialItem.ProviderID == providerID {
			items = append(items, credentialItem)
		}
	}
	return items, nil
}

func (s *stubCredentialBackend) UpdateDetails(_ context.Context, identifier string, label *string, disabled *bool, data map[string]any) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	credentialItem, ok := s.credentials[identifier]
	if !ok {
		return apierrors.NewNotFoundError("credential", identifier)
	}
	if label != nil {
		credentialItem.Label = *label
	}
	if disabled != nil {
		credentialItem.Disabled = *disabled
	}
	if data != nil {
		credentialItem.Data = data
	}
	return nil
}

func (s *stubCredentialBackend) Add(_ context.Context, options credential.AddOptions) (*models.Credential, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.counter++
	credentialItem := &models.Credential{
		ID: fmt.Sprintf("cred-%d", s.counter), ProviderID: options.ProviderID,
		Label: options.Label, Data: options.Data,
	}
	s.credentials[credentialItem.ID] = credentialItem
	return credentialItem, nil
}

// stubVirtualBackend is an in-memory GormVirtualBackend.
type stubVirtualBackend struct {
	mutex       sync.Mutex
	models      map[string]*models.VirtualModel
	updateError error
}

func newStubVirtualBackend() *stubVirtualBackend {
	return &stubVirtualBackend{models: map[string]*models.VirtualModel{}}
}

func (s *stubVirtualBackend) List(_ context.Context) ([]*models.VirtualModel, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	items := make([]*models.VirtualModel, 0, len(s.models))
	for _, virtualModel := range s.models {
		items = append(items, virtualModel)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (s *stubVirtualBackend) Get(_ context.Context, identifier string) (*models.VirtualModel, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	virtualModel, ok := s.models[identifier]
	if !ok {
		return nil, apierrors.NewNotFoundError("virtual model", identifier)
	}
	return virtualModel, nil
}

func (s *stubVirtualBackend) Create(_ context.Context, virtualModel *models.VirtualModel) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if virtualModel.ID == "" {
		virtualModel.ID = strings.ToLower(strings.ReplaceAll(virtualModel.Name, " ", "-"))
	}
	if _, taken := s.models[virtualModel.ID]; taken {
		return fmt.Errorf("virtual model %q already exists: %w", virtualModel.Name, apierrors.ErrAlreadyExists)
	}
	s.models[virtualModel.ID] = virtualModel
	return nil
}

func (s *stubVirtualBackend) Update(_ context.Context, identifier string, virtualModel *models.VirtualModel) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.updateError != nil {
		return s.updateError
	}
	if _, ok := s.models[identifier]; !ok {
		return apierrors.NewNotFoundError("virtual model", identifier)
	}
	virtualModel.ID = identifier
	s.models[identifier] = virtualModel
	return nil
}

func (s *stubVirtualBackend) Delete(_ context.Context, identifier string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if _, ok := s.models[identifier]; !ok {
		return fmt.Errorf("virtual model %q: %w", identifier, apierrors.ErrNotFound)
	}
	delete(s.models, identifier)
	return nil
}

// stubTokenBackend is an in-memory GormTokenBackend.
type stubTokenBackend struct {
	mutex   sync.Mutex
	tokens  map[string]*models.RouterToken
	counter int
}

func newStubTokenBackend() *stubTokenBackend {
	return &stubTokenBackend{tokens: map[string]*models.RouterToken{}}
}

func (s *stubTokenBackend) List(_ context.Context) ([]*models.RouterToken, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	items := make([]*models.RouterToken, 0, len(s.tokens))
	for _, tokenRecord := range s.tokens {
		items = append(items, tokenRecord)
	}
	return items, nil
}

func (s *stubTokenBackend) UpdateRules(_ context.Context, identifier string, rules models.TokenRules) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	tokenRecord, ok := s.tokens[identifier]
	if !ok {
		return fmt.Errorf("token %q: %w", identifier, apierrors.ErrNotFound)
	}
	tokenRecord.Rules = rules
	return nil
}

func (s *stubTokenBackend) Create(_ context.Context, options token.CreateOptions) (*models.RouterToken, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.counter++
	tokenRecord := &models.RouterToken{
		ID: fmt.Sprintf("token-%d", s.counter), Name: options.Name, Rules: options.Rules,
	}
	s.tokens[tokenRecord.ID] = tokenRecord
	return tokenRecord, nil
}

func (s *stubTokenBackend) Delete(_ context.Context, identifier string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if _, ok := s.tokens[identifier]; !ok {
		return fmt.Errorf("token %q: %w", identifier, apierrors.ErrNotFound)
	}
	delete(s.tokens, identifier)
	return nil
}

// stubModelInfoBackend is an in-memory GormModelInfoBackend.
type stubModelInfoBackend struct {
	mutex     sync.Mutex
	overrides map[string]*models.ModelOverride
	snapshots map[string]bool
}

func newStubModelInfoBackend() *stubModelInfoBackend {
	return &stubModelInfoBackend{
		overrides: map[string]*models.ModelOverride{},
		snapshots: map[string]bool{},
	}
}

func (s *stubModelInfoBackend) ListOverrides(_ context.Context, providerID string) ([]*models.ModelOverride, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	var items []*models.ModelOverride
	for _, override := range s.overrides {
		if override.ProviderID == providerID {
			items = append(items, override)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

func (s *stubModelInfoBackend) SetOverride(_ context.Context, override models.ModelOverride) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if override.ProviderID == "" || override.Name == "" {
		return fmt.Errorf("override requires provider id and model name: %w", apierrors.ErrInvalidInput)
	}
	stored := override
	s.overrides[override.ProviderID+"/"+override.Name] = &stored
	return nil
}

func (s *stubModelInfoBackend) DeleteOverride(_ context.Context, providerID, modelName string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	key := providerID + "/" + modelName
	if _, ok := s.overrides[key]; !ok {
		return fmt.Errorf("model override %q: %w", key, apierrors.ErrNotFound)
	}
	delete(s.overrides, key)
	return nil
}

func (s *stubModelInfoBackend) DeleteSnapshot(_ context.Context, providerID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if !s.snapshots[providerID] {
		return fmt.Errorf("model info snapshot %q: %w", providerID, apierrors.ErrNotFound)
	}
	delete(s.snapshots, providerID)
	return nil
}

// stubPluginBackend is an in-memory GormPluginBackend. Deletes cascade
// implicitly: no storage table exists here, so removing the plugin row is
// the whole clear, matching the cascade discipline.
type stubPluginBackend struct {
	mutex   sync.Mutex
	plugins map[string]*luaplugin.PluginRecord
	counter int
}

func newStubPluginBackend() *stubPluginBackend {
	return &stubPluginBackend{plugins: map[string]*luaplugin.PluginRecord{}}
}

func (s *stubPluginBackend) ListSQLite(_ context.Context) ([]*luaplugin.PluginRecord, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	items := make([]*luaplugin.PluginRecord, 0, len(s.plugins))
	for _, plugin := range s.plugins {
		items = append(items, plugin)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (s *stubPluginBackend) InstallSQLite(_ context.Context, source []byte, origin luaplugin.PluginOrigin) (*luaplugin.PluginRecord, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if len(source) == 0 {
		return nil, fmt.Errorf("plugin source is empty")
	}
	s.counter++
	plugin := &luaplugin.PluginRecord{
		ID: fmt.Sprintf("plugin-%d", s.counter), Source: source, Origin: origin,
	}
	s.plugins[plugin.ID] = plugin
	return plugin, nil
}

func (s *stubPluginBackend) DeleteSQLite(_ context.Context, pluginID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if _, ok := s.plugins[pluginID]; !ok {
		return fmt.Errorf("plugin %q: %w", pluginID, apierrors.ErrNotFound)
	}
	delete(s.plugins, pluginID)
	return nil
}

func (s *stubPluginBackend) SetDisabled(_ context.Context, pluginID string, disabled bool) (*luaplugin.PluginRecord, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	plugin, ok := s.plugins[pluginID]
	if !ok {
		return nil, fmt.Errorf("plugin %q: %w", pluginID, apierrors.ErrNotFound)
	}
	plugin.Disabled = disabled
	return plugin, nil
}

// stubRepositoryBackend is an in-memory GormRepositoryBackend with the
// builtin protection of the real service.
type stubRepositoryBackend struct {
	mutex        sync.Mutex
	repositories map[string]*pluginrepo.RepoRecord
}

func newStubRepositoryBackend() *stubRepositoryBackend {
	return &stubRepositoryBackend{repositories: map[string]*pluginrepo.RepoRecord{}}
}

func (s *stubRepositoryBackend) List() ([]*pluginrepo.RepoRecord, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	items := make([]*pluginrepo.RepoRecord, 0, len(s.repositories))
	for _, repository := range s.repositories {
		items = append(items, repository)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (s *stubRepositoryBackend) AddRepo(_ context.Context, rawURL string) (*pluginrepo.RepoRecord, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if strings.TrimSpace(rawURL) == "" {
		return nil, fmt.Errorf("invalid repository url")
	}
	identifier := "index/" + strings.ToLower(strings.ReplaceAll(rawURL, "https://", ""))
	if existing, ok := s.repositories[identifier]; ok {
		return existing, nil
	}
	repository := &pluginrepo.RepoRecord{ID: identifier, Kind: "index", IndexURL: rawURL, SourceURL: rawURL}
	s.repositories[identifier] = repository
	return repository, nil
}

func (s *stubRepositoryBackend) Remove(identifier string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	repository, ok := s.repositories[identifier]
	if !ok {
		return fmt.Errorf("remove repo %q: %w", identifier, apierrors.ErrNotFound)
	}
	if repository.Builtin {
		return fmt.Errorf("remove repo %q: %w", identifier, pluginrepo.ErrBuiltinRepoProtected)
	}
	delete(s.repositories, identifier)
	return nil
}

// gormDataStack wires a GormService over fresh in-memory backends.
func gormDataStack() (*GormService, *stubProviderBackend, *stubCredentialBackend, *stubVirtualBackend, *stubTokenBackend, *stubPluginBackend, *stubRepositoryBackend, *stubModelInfoBackend) {
	providers := newStubProviderBackend()
	credentials := newStubCredentialBackend()
	virtualModels := newStubVirtualBackend()
	tokens := newStubTokenBackend()
	plugins := newStubPluginBackend()
	repositories := newStubRepositoryBackend()
	modelInfo := newStubModelInfoBackend()
	service := NewGorm(providers, credentials, virtualModels, tokens, plugins, repositories, modelInfo)
	return service, providers, credentials, virtualModels, tokens, plugins, repositories, modelInfo
}

func TestGormImportProvidersRemapsCollisionIDs(t *testing.T) {
	service, providers, credentials, _, _, _, _, modelInfo := gormDataStack()
	ctx := context.Background()
	if _, err := providers.Create(ctx, provider.CreateOptions{Name: "Occupant", TypeKey: "dm-type", Qualifier: "q"}); err != nil {
		t.Fatalf("create occupant: %v", err)
	}
	bundle := &ProviderBundle{
		Instance: &models.ProviderInstance{
			ID: "dm-type:elsewhere", Name: "X", TypeKey: "dm-type", Qualifier: "q",
		},
		Credentials: []*models.Credential{
			{ID: "cred-1", Label: "k", Data: map[string]any{"api_key": "v"}},
		},
		Overrides: []*models.ModelOverride{
			{ProviderID: "dm-type:elsewhere", Name: "m"},
		},
	}
	if err := service.ImportProviders(ctx, []*ProviderBundle{bundle}); err != nil {
		t.Fatalf("import: %v", err)
	}
	imported, err := providers.Get(ctx, "dm-type:q-2")
	if err != nil {
		t.Fatalf("imported instance must own the dedup ID dm-type:q-2: %v", err)
	}
	if imported.TypeKey != "dm-type" {
		t.Fatalf("imported type = %q, want dm-type", imported.TypeKey)
	}
	providerCredentials, err := credentials.ListByProvider(ctx, "dm-type:q-2")
	if err != nil || len(providerCredentials) != 1 {
		t.Fatalf("imported credentials = %v, %v; want one under dm-type:q-2", providerCredentials, err)
	}
	overrides, err := modelInfo.ListOverrides(ctx, "dm-type:q-2")
	if err != nil || len(overrides) != 1 {
		t.Fatalf("imported overrides = %v, %v; want one under dm-type:q-2", overrides, err)
	}
}

func TestGormImportProvidersUpdateErrorSurfaces(t *testing.T) {
	service, providers, _, _, _, _, _, _ := gormDataStack()
	ctx := context.Background()
	created, err := providers.Create(ctx, provider.CreateOptions{Name: "Original", TypeKey: "dm-type", Qualifier: "q"})
	if err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	providers.updateError = fmt.Errorf("storage unavailable")
	bundle := &ProviderBundle{
		Instance: &models.ProviderInstance{ID: created.ID, Name: "Changed", TypeKey: "dm-type", Qualifier: "q"},
	}
	if err := service.ImportProviders(ctx, []*ProviderBundle{bundle}); err == nil {
		t.Fatal("import succeeded, want the provider update error")
	}
}

func TestGormPurgeProviderRemovesOverrides(t *testing.T) {
	service, providers, _, _, _, _, _, modelInfo := gormDataStack()
	ctx := context.Background()
	created, err := providers.Create(ctx, provider.CreateOptions{Name: "Purge", TypeKey: "dm-type", Qualifier: "q"})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	modelInfo.snapshots[created.ID] = true
	if err := modelInfo.SetOverride(ctx, models.ModelOverride{ProviderID: created.ID, Name: "m"}); err != nil {
		t.Fatalf("set override: %v", err)
	}
	if err := service.PurgeProvider(ctx, created.ID); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if overrides, err := modelInfo.ListOverrides(ctx, created.ID); err != nil || len(overrides) != 0 {
		t.Fatalf("overrides after purge = %v, %v; want none", overrides, err)
	}
	if modelInfo.snapshots[created.ID] {
		t.Fatal("snapshot after purge still present, want it removed")
	}
	if _, err := providers.Get(ctx, created.ID); err == nil {
		t.Fatal("provider after purge still present, want it removed")
	}
}

func TestGormPurgeProviderMissingSnapshotStaysSilent(t *testing.T) {
	service, providers, _, virtualModels, _, _, _, _ := gormDataStack()
	ctx := context.Background()
	created, err := providers.Create(ctx, provider.CreateOptions{Name: "Purge", TypeKey: "dm-type", Qualifier: "q"})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	managed := &models.VirtualModel{
		ID: "managed", Name: "Managed", ManagedBy: virtual.ProviderMarker(created.ID, "chat"),
		Models: []models.VirtualModelEntry{{ModelID: "other/m"}},
	}
	virtualModels.models[managed.ID] = managed
	manual := &models.VirtualModel{
		ID: "manual", Name: "Manual",
		Models: []models.VirtualModelEntry{{ModelID: "other/m"}},
	}
	virtualModels.models[manual.ID] = manual
	if err := service.PurgeProvider(ctx, created.ID); err != nil {
		t.Fatalf("purge without snapshot: %v", err)
	}
	if _, err := virtualModels.Get(ctx, "managed"); err == nil {
		t.Fatal("managed virtual model still present, want it purged")
	}
	if _, err := virtualModels.Get(ctx, "manual"); err != nil {
		t.Fatalf("manual virtual model missing after purge: %v", err)
	}
}

func TestGormImportVirtualModelsUpdateErrorSurfaces(t *testing.T) {
	service, _, _, virtualModels, _, _, _, _ := gormDataStack()
	ctx := context.Background()
	seeded := &models.VirtualModel{
		ID: "vm-one", Name: "One",
		Models: []models.VirtualModelEntry{{ModelID: "other/m"}},
	}
	virtualModels.models[seeded.ID] = seeded
	virtualModels.updateError = fmt.Errorf("storage unavailable")
	incoming := &models.VirtualModel{
		ID: "vm-one", Name: "One",
		Models: []models.VirtualModelEntry{{ModelID: "other/m"}},
	}
	if err := service.ImportVirtualModels(ctx, []*models.VirtualModel{incoming}); err == nil {
		t.Fatal("import succeeded, want the virtual update error")
	}
}

func TestGormClearPluginsDeletesPluginsKeepsBuiltin(t *testing.T) {
	service, _, _, _, _, plugins, repositories, _ := gormDataStack()
	ctx := context.Background()
	if _, err := plugins.InstallSQLite(ctx, []byte("source-a"), luaplugin.PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install plugin: %v", err)
	}
	repositories.repositories["builtin"] = &pluginrepo.RepoRecord{ID: "builtin", Builtin: true}
	repositories.repositories["custom"] = &pluginrepo.RepoRecord{ID: "custom"}
	if err := service.ClearPlugins(ctx); err != nil {
		t.Fatalf("clear plugins: %v", err)
	}
	remaining, err := plugins.ListSQLite(ctx)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("plugins after clear = %v, %v; want none", remaining, err)
	}
	kept, err := repositories.List()
	if err != nil || len(kept) != 1 || kept[0].ID != "builtin" {
		t.Fatalf("repos after clear = %v, %v; want only builtin", kept, err)
	}
}

func TestGormStatsCountsSubsystems(t *testing.T) {
	service, providers, credentials, virtualModels, tokens, plugins, repositories, _ := gormDataStack()
	ctx := context.Background()
	if _, err := providers.Create(ctx, provider.CreateOptions{Name: "P", TypeKey: "dm-type", Qualifier: "a"}); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	if _, err := credentials.Add(ctx, credential.AddOptions{ProviderID: "dm-type:a", Label: "k", Data: map[string]any{}}); err != nil {
		t.Fatalf("add credential: %v", err)
	}
	virtualModels.models["vm"] = &models.VirtualModel{ID: "vm", Name: "Vm"}
	if _, err := tokens.Create(ctx, token.CreateOptions{Name: "T"}); err != nil {
		t.Fatalf("create token: %v", err)
	}
	if _, err := plugins.InstallSQLite(ctx, []byte("source"), luaplugin.PluginOrigin{}); err != nil {
		t.Fatalf("install plugin: %v", err)
	}
	repositories.repositories["repo"] = &pluginrepo.RepoRecord{ID: "repo"}
	statistics, err := service.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if statistics.Providers != 1 || statistics.Credentials != 1 || statistics.VirtualModels != 1 || statistics.Tokens != 1 || statistics.Plugins != 1 || statistics.PluginRepos != 1 {
		t.Fatalf("stats = %+v, want one of each", statistics)
	}
}

func TestGormImportTokensUpdatesKnownMintsUnknown(t *testing.T) {
	service, _, _, _, tokens, _, _, _ := gormDataStack()
	ctx := context.Background()
	known, err := tokens.Create(ctx, token.CreateOptions{Name: "Known"})
	if err != nil {
		t.Fatalf("seed token: %v", err)
	}
	rules := models.TokenRules{AllowedModels: []models.ModelId{"dm-type/m"}}
	if err := service.ImportTokens(ctx, []*models.RouterToken{
		{ID: known.ID, Name: "Known", Rules: rules},
		{Name: "Fresh", Rules: models.TokenRules{}},
	}); err != nil {
		t.Fatalf("import tokens: %v", err)
	}
	listed, err := tokens.List(ctx)
	if err != nil || len(listed) != 2 {
		t.Fatalf("tokens after import = %v, %v; want two", listed, err)
	}
	updated, ok := tokens.tokens[known.ID]
	if !ok || len(updated.Rules.AllowedModels) != 1 {
		t.Fatalf("known token rules = %+v, want the imported allow-list", updated.Rules)
	}
}

func TestGormExportImportProviderRoundTrip(t *testing.T) {
	service, _, credentials, _, _, _, _, modelInfo := gormDataStack()
	ctx := context.Background()
	bundle := &ProviderBundle{
		Instance: &models.ProviderInstance{ID: "dm-type:rt", Name: "RoundTrip", TypeKey: "dm-type", Qualifier: "rt"},
		Credentials: []*models.Credential{
			{Label: "k", Data: map[string]any{"api_key": "v"}},
		},
		Overrides: []*models.ModelOverride{
			{ProviderID: "dm-type:rt", Name: "m"},
		},
	}
	if err := service.ImportProviders(ctx, []*ProviderBundle{bundle}); err != nil {
		t.Fatalf("import: %v", err)
	}
	exported, err := service.ExportProvider(ctx, "dm-type:rt")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if exported.Instance == nil || exported.Instance.ID != "dm-type:rt" {
		t.Fatalf("exported instance = %+v, want dm-type:rt", exported.Instance)
	}
	_ = credentials
	_ = modelInfo
	if len(exported.Credentials) != 1 {
		t.Fatalf("exported credentials = %d, want one", len(exported.Credentials))
	}
	if len(exported.Overrides) != 1 {
		t.Fatalf("exported overrides = %d, want one", len(exported.Overrides))
	}
}

// TestGormBackendsSatisfyInterfaces pins the narrow-interface promise: the
// real GORM services plug into GormService without adapters. Types only;
// no storage runs here.
func TestGormBackendsSatisfyInterfaces(t *testing.T) {
	var _ GormProviderBackend = (*provider.GormService)(nil)
	var _ GormCredentialBackend = (*credential.GormService)(nil)
	var _ GormVirtualBackend = (*virtual.GormService)(nil)
	var _ GormTokenBackend = (*token.GormService)(nil)
	var _ GormModelInfoBackend = (*modelinfo.GormService)(nil)
	var _ GormPluginBackend = (*luaplugin.Service)(nil)
	var _ GormRepositoryBackend = (*pluginrepo.Service)(nil)
}

func TestGormExportVirtualModelsSkipsManaged(t *testing.T) {
	service, _, _, virtualModels, _, _, _, _ := gormDataStack()
	ctx := context.Background()
	virtualModels.models["managed"] = &models.VirtualModel{
		ID: "managed", Name: "Managed", ManagedBy: virtual.ProviderMarker("provider", "chat"),
	}
	virtualModels.models["manual"] = &models.VirtualModel{ID: "manual", Name: "Manual"}

	exported, err := service.ExportVirtualModels(ctx)
	if err != nil {
		t.Fatalf("export virtual models: %v", err)
	}
	if len(exported) != 1 || exported[0].ID != "manual" {
		t.Fatalf("exported = %+v, want only the manual virtual model", exported)
	}
}

func TestGormImportVirtualModelsSkipsManaged(t *testing.T) {
	service, _, _, virtualModels, _, _, _, _ := gormDataStack()
	ctx := context.Background()

	err := service.ImportVirtualModels(ctx, []*models.VirtualModel{
		{ID: "managed", Name: "Managed", ManagedBy: virtual.ProviderMarker("missing", "chat")},
		{ID: "manual", Name: "Manual"},
	})
	if err != nil {
		t.Fatalf("import virtual models: %v", err)
	}
	if _, err := virtualModels.Get(ctx, "managed"); err == nil {
		t.Fatal("managed virtual model was imported, want it skipped")
	}
	if _, err := virtualModels.Get(ctx, "manual"); err != nil {
		t.Fatalf("manual virtual model missing after import: %v", err)
	}
}

type fakeProxyBackend struct {
	bundle  *models.ProxyDataBundle
	cleared bool
}

func (f *fakeProxyBackend) DataCounts(context.Context) (int, int, error) {
	return len(f.bundle.Pools), len(f.bundle.Proxies), nil
}

func (f *fakeProxyBackend) ExportData(context.Context) (*models.ProxyDataBundle, error) {
	return f.bundle, nil
}

func (f *fakeProxyBackend) ImportData(_ context.Context, bundle *models.ProxyDataBundle) error {
	f.bundle = bundle
	return nil
}

func (f *fakeProxyBackend) ClearData(context.Context) error {
	f.cleared = true
	return nil
}

func TestGormProxyDataRoundTripThroughBackend(t *testing.T) {
	service, _, _, _, _, _, _, _ := gormDataStack()
	ctx := context.Background()

	if _, err := service.ExportProxy(ctx); err == nil {
		t.Fatal("export without a proxy backend succeeded, want an error")
	}

	backend := &fakeProxyBackend{bundle: &models.ProxyDataBundle{
		Version: models.ProxyDataVersion,
		Pools:   []models.CustomProxyPool{{ID: "mine", Name: "Mine"}},
		Proxies: []models.ProxyDataEntry{{URL: "http://203.0.113.1:8080"}, {URL: "http://203.0.113.2:8080"}},
	}}
	service.SetProxyBackend(backend)

	stats, err := service.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.ProxyPools != 1 || stats.Proxies != 2 {
		t.Fatalf("proxy stats = %d pools, %d proxies; want 1 and 2", stats.ProxyPools, stats.Proxies)
	}
	exported, err := service.ExportProxy(ctx)
	if err != nil || exported != backend.bundle {
		t.Fatalf("export = %v, %v; want the backend bundle", exported, err)
	}
	incoming := &models.ProxyDataBundle{Version: models.ProxyDataVersion}
	if err := service.ImportProxy(ctx, incoming); err != nil || backend.bundle != incoming {
		t.Fatalf("import stored %v, err %v; want the incoming bundle", backend.bundle, err)
	}
	if err := service.ClearProxy(ctx); err != nil || !backend.cleared {
		t.Fatalf("clear = %v, cleared %v; want success", err, backend.cleared)
	}
}

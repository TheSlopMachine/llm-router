package provider

import (
	"errors"
	"fmt"
	"sort"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
)

// TypeKeys returns every known provider type: Go backends plus enabled
// Lua plugin type keys.
func (s *Service) TypeKeys() []string {
	seen := map[string]bool{}
	var out []string
	for _, k := range s.GoAdapterTypes() {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	if s.luaSvc != nil {
		for _, k := range s.luaSvc.Registered() {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}

// IsTypeAvailable reports whether a backend serves typeKey right now:
// a built-in Go adapter or a currently registered Lua plugin type.
// Rows with unavailable types stay in the database so reinstalling the
// plugin restores them, but the dashboard hides them.
func (s *Service) IsTypeAvailable(typeKey string) bool {
	if _, ok := s.GoAdapterFor(typeKey); ok {
		return true
	}
	if s.luaSvc == nil {
		return false
	}
	_, err := s.luaSvc.Lookup(typeKey)
	return err == nil
}

// SupportsAuthFlow reports whether a type offers a multi-step auth wizard.
func (s *Service) SupportsAuthFlow(typeKey string) bool {
	if _, ok := s.GoAdapterFor(typeKey); ok {
		return false
	}
	if s.luaSvc == nil {
		return false
	}
	return s.luaSvc.HasHandler(typeKey, "auth_initiate")
}

// IsCreatableTypeKey reports whether users may create provider rows of a
// type through the UI. The agents singleton is core-managed and excluded;
// every other known type stays creatable (qualifier rows included).
func IsCreatableTypeKey(typeKey string) bool {
	return typeKey != TypeVirtual
}

// ConfigSchema returns the config UI tree for a type key.
// Go types serve static trees; Lua types delegate to config_schema.
// ErrHandlerNotFound-equivalent (nil, nil) means "raw JSON fallback".
func (s *Service) ConfigSchema(typeKey string) ([]*models.UINode, error) {
	if typeKey == TypeCustom {
		return []*models.UINode{
			{Type: "text", Text: "OpenAI-compatible endpoint details."},
			{Type: "input", Name: "base_url", Label: "Base URL", Required: true, Placeholder: "https://api.example.com/v1"},
		}, nil
	}
	if typeKey == TypeVirtual {
		return nil, nil
	}
	if s.luaSvc == nil {
		return nil, fmt.Errorf("no plugin service wired")
	}
	nodes, err := s.luaSvc.Schema(typeKey, "config_schema")
	if err != nil {
		if errors.Is(err, luaplugin.ErrHandlerNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return nodes, nil
}

// CredentialSchema returns the credential UI tree for a type key.
func (s *Service) CredentialSchema(typeKey string) ([]*models.UINode, error) {
	if typeKey == TypeCustom {
		return []*models.UINode{
			{Type: "text", Text: "Enter the API key for this provider."},
			{Type: "input", Name: "api_key", InputType: "password", Label: "API Key", Required: true},
			{Type: "button", Text: "Save", FormAction: "submit"},
		}, nil
	}
	if typeKey == TypeVirtual {
		return []*models.UINode{
			{Type: "text", Text: "Bind this credential to a virtual model."},
			{Type: "input", Name: "agent_id", Label: "Virtual model ID", Required: true},
			{Type: "button", Text: "Save", FormAction: "submit"},
		}, nil
	}
	if s.luaSvc == nil {
		return nil, fmt.Errorf("no plugin service wired")
	}
	nodes, err := s.luaSvc.Schema(typeKey, "credential_schema")
	if err != nil {
		if errors.Is(err, luaplugin.ErrHandlerNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return nodes, nil
}

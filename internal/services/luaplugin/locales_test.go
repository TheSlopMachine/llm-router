package luaplugin

import (
	"context"
	"strings"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func TestNormalizeLocale(t *testing.T) {
	cases := map[string]string{
		"":       "en",
		"en":     "en",
		"EN":     "en",
		"en-US":  "en",
		"ru":     "ru",
		"RU":     "ru",
		"ru-RU":  "ru",
		"ru_UA":  "ru",
		"de":     "en",
		"fr-FR":  "en",
		"  ru  ": "ru",
	}
	for input, want := range cases {
		if got := NormalizeLocale(input); got != want {
			t.Errorf("NormalizeLocale(%q): got %q, want %q", input, got, want)
		}
	}
}

func TestNegotiateLocale(t *testing.T) {
	if got := NegotiateLocale("ru", "en-US"); got != "ru" {
		t.Errorf("query must win: got %q", got)
	}
	if got := NegotiateLocale("", "ru-RU,ru;q=0.9,en;q=0.8"); got != "ru" {
		t.Errorf("header fallback: got %q", got)
	}
	if got := NegotiateLocale("", "en-US,en;q=0.9"); got != "en" {
		t.Errorf("header en: got %q", got)
	}
	if got := NegotiateLocale("", ""); got != "en" {
		t.Errorf("empty must resolve en: got %q", got)
	}
}

const localePluginSource = `--- @plugin Locale Probe
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("locprobe", {
  complete = function() end,
  locales = {
    ["en"] = {
      ["misc.api_key"] = "API Key",
      ["misc.save"] = "Save",
    },
    ["ru"] = {
      ["misc.api_key"] = "API-ключ",
    },
  },
  credential_schema = {
    { type = "secret", name = "api_key", label = "misc.api_key" },
    { type = "select", name = "mode", label = "Mode",
      options = { "fast", "slow" },
      option_labels = { ["fast"] = "misc.save" } },
    { type = "section", title = "misc.api_key", subtitle = "Plain subtitle",
      content = { { type = "text", text = "misc.save" } } },
    { type = "button", text = "misc.save", form_action = "submit" },
  },
})
`

func installLocaleProbe(t *testing.T) *Service {
	t.Helper()
	service, err := New(testutil.SetupTestSQLite(t), nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, err := service.InstallSQLite(context.Background(), []byte(localePluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install locale probe: %v", err)
	}
	return service
}

func TestSchemaLocaleResolution(t *testing.T) {
	service := installLocaleProbe(t)
	english, err := service.Schema("locprobe", "credential_schema", "en")
	if err != nil {
		t.Fatalf("schema en: %v", err)
	}
	if english[0].Label != "API Key" {
		t.Errorf("en label: got %q", english[0].Label)
	}
	russian, err := service.Schema("locprobe", "credential_schema", "ru")
	if err != nil {
		t.Fatalf("schema ru: %v", err)
	}
	if russian[0].Label != "API-ключ" {
		t.Errorf("ru label: got %q", russian[0].Label)
	}
	if russian[3].Text != "Save" {
		t.Errorf("ru fallback to en: got %q", russian[3].Text)
	}
	if russian[1].Label != "Mode" {
		t.Errorf("verbatim fallback: got %q", russian[1].Label)
	}
	if russian[1].OptionLabels["fast"] != "Save" {
		t.Errorf("option label fallback: got %q", russian[1].OptionLabels["fast"])
	}
	section := russian[2]
	if section.Title != "API-ключ" || len(section.Content) != 1 || section.Content[0].Text != "Save" {
		t.Errorf("nested resolution wrong: %+v", section)
	}
	again, err := service.Schema("locprobe", "credential_schema", "en")
	if err != nil {
		t.Fatalf("schema en again: %v", err)
	}
	if again[0].Label != "API Key" {
		t.Errorf("stored tree mutated: got %q", again[0].Label)
	}
}

func TestParseLocalesInvalid(t *testing.T) {
	bodies := map[string]string{
		"non-table locales": `locales = "x",`,
		"unknown locale":    `locales = { ["de"] = { ["a"] = "b" } },`,
		"non-table locale":  `locales = { ["en"] = "x" },`,
		"empty key":         `locales = { ["en"] = { ["  "] = "b" } },`,
		"empty value":       `locales = { ["en"] = { ["a"] = "  " } },`,
		"non-string value":  `locales = { ["en"] = { ["a"] = 42 } },`,
		"oversize key":      `locales = { ["en"] = { ["` + strings.Repeat("k", 129) + `"] = "b" } },`,
		"oversize value":    `locales = { ["en"] = { ["a"] = "` + strings.Repeat("v", 1001) + `" } },`,
	}
	for name, localesBody := range bodies {
		service, err := New(testutil.SetupTestSQLite(t), nil)
		if err != nil {
			t.Fatalf("%s: new service: %v", name, err)
		}
		src := `--- @plugin Bad Locale
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("badloc", {
  complete = function() end,
  ` + localesBody + `
  credential_schema = { { type = "text", text = "hi" } },
})
`
		if _, err := service.InstallSQLite(context.Background(), []byte(src), PluginOrigin{Manual: true}); err == nil {
			t.Errorf("%s: expected install error", name)
		}
	}
}

const localeAuthSource = `--- @plugin Locale Auth
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("locauth", {
  complete = function() end,
  locales = {
    ["en"] = { ["misc.title"] = "Sign in" },
    ["ru"] = { ["misc.title"] = "Войти" },
  },
  auth_initiate = function(ctx)
    return { render = { { type = "text", text = "misc.title" } } }
  end,
  auth_step = function(ctx, input)
    return { render = { { type = "text", text = "misc.title" } } }
  end,
})
`

func TestAuthLocaleResolution(t *testing.T) {
	service, err := New(testutil.SetupTestSQLite(t), nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, err := service.InstallSQLite(context.Background(), []byte(localeAuthSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	english, err := service.AuthInitiate(context.Background(), "locauth", "flow1", nil, "en")
	if err != nil {
		t.Fatalf("initiate en: %v", err)
	}
	if len(english.Render) != 1 || english.Render[0].Text != "Sign in" {
		t.Fatalf("en render wrong: %+v", english.Render)
	}
	russian, err := service.AuthInitiate(context.Background(), "locauth", "flow1", nil, "ru")
	if err != nil {
		t.Fatalf("initiate ru: %v", err)
	}
	if len(russian.Render) != 1 || russian.Render[0].Text != "Войти" {
		t.Fatalf("ru render wrong: %+v", russian.Render)
	}
	stepped, err := service.AuthStep(context.Background(), "locauth", "flow1", "next", map[string]any{}, nil, "ru")
	if err != nil {
		t.Fatalf("step ru: %v", err)
	}
	if len(stepped.Render) != 1 || stepped.Render[0].Text != "Войти" {
		t.Fatalf("step ru render wrong: %+v", stepped.Render)
	}
}

func TestLocalesPersistAndRollback(t *testing.T) {
	service, err := New(testutil.SetupTestSQLite(t), nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	ctx := context.Background()
	first, err := service.InstallSQLite(ctx, []byte(localePluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install v1: %v", err)
	}
	if first.Locales["locprobe"]["ru"]["misc.api_key"] != "API-ключ" {
		t.Fatalf("v1 locales missing: %+v", first.Locales)
	}
	secondSource := strings.Replace(localePluginSource, "@version 1.0.0", "@version 2.0.0", 1)
	secondSource = strings.Replace(secondSource, `"API-ключ"`, `"Ключ"`, 1)
	if _, err := service.InstallSQLite(ctx, []byte(secondSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install v2: %v", err)
	}
	russian, err := service.Schema("locprobe", "credential_schema", "ru")
	if err != nil {
		t.Fatalf("schema v2 ru: %v", err)
	}
	if russian[0].Label != "Ключ" {
		t.Fatalf("v2 label: got %q", russian[0].Label)
	}
	rolled, err := service.RollbackSQLite(ctx, first.ID)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if rolled.Locales["locprobe"]["ru"]["misc.api_key"] != "API-ключ" {
		t.Fatalf("rollback locales wrong: %+v", rolled.Locales)
	}
	restored, err := service.Schema("locprobe", "credential_schema", "ru")
	if err != nil {
		t.Fatalf("schema rollback ru: %v", err)
	}
	if restored[0].Label != "API-ключ" {
		t.Fatalf("restored label: got %q", restored[0].Label)
	}
}

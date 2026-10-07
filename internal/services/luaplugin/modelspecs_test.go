package luaplugin

import (
	"reflect"
	"strings"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

const specPluginSource = `--- @plugin Spec Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("spec-type", {
  complete = function(ctx, request)
    return {
      id = "chatcmpl-spec", object = "chat.completion", created = 1700000000,
      model = request.model,
      choices = {
        { index = 0, message = { role = "assistant", content = "hi" }, finish_reason = "stop" },
      },
      usage = { prompt_tokens = 1, completion_tokens = 1, total_tokens = 2 },
    }
  end,

  get_model_infos = function(ctx)
    return {
      { name = "m-one", display_name = "One", context_window = 1000 },
      { name = "m-two", display_name = "Two" },
    }
  end,

  model_specs = {
    ["m-one"] = { context_window = 200000, rpm = 60 },
    ["m-ghost"] = { context_window = 7 },
  },
})
`

func TestInstallRejectsBadModelSpecs(t *testing.T) {
	svc := setupService(t)
	nonTable := `--- @plugin Spec Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @allow_host example.com

llm_router.register("spec-type", {
  complete = function(ctx, request) return nil, { message = "x", code = "server_error" } end,
  model_specs = 42,
})
`
	if _, err := svc.Install([]byte(nonTable), PluginOrigin{Manual: true}); err == nil {
		t.Error("non-table specs: expected install error")
	}
	cases := map[string]func(string) string{
		"unknown field": func(src string) string {
			return strings.Replace(src, "context_window = 200000", "context_windows = 200000", 1)
		},
		"name field": func(src string) string {
			return strings.Replace(src, "rpm = 60", `name = "m-one", rpm = 60`, 1)
		},
		"mistyped value": func(src string) string {
			return strings.Replace(src, "context_window = 200000", `context_window = "huge"`, 1)
		},
	}
	for name, mutate := range cases {
		if _, err := svc.Install([]byte(mutate(specPluginSource)), PluginOrigin{Manual: true}); err == nil {
			t.Errorf("%s: expected install error", name)
		}
	}
}

func TestGetModelInfosAppliesSpecs(t *testing.T) {
	svc := setupService(t)
	if _, err := svc.Install([]byte(specPluginSource), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	infos, err := svc.GetModelInfos(t.Context(), "spec-provider", "spec-type", nil)
	if err != nil {
		t.Fatalf("get_model_infos: %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("rows: got %d want 2 (ghost specs never resurrect)", len(infos))
	}
	one := infos[0]
	if one.Name != "m-one" || one.DisplayName != "One" {
		t.Fatalf("identity must pass through: %+v", one)
	}
	if one.ContextWindow != 200000 || one.RPM != 60 {
		t.Fatalf("spec must win: %+v", one)
	}
	two := infos[1]
	if two.Name != "m-two" || two.ContextWindow != 0 {
		t.Fatalf("unspecced rows must pass through: %+v", two)
	}
}

func TestModelSpecFieldsMatchModelInfo(t *testing.T) {
	want := map[string]bool{}
	typ := reflect.TypeOf(models.ModelInfo{})
	for i := 0; i < typ.NumField(); i++ {
		name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if name == "" || name == "-" || name == "name" {
			continue
		}
		want[name] = true
	}
	if !reflect.DeepEqual(modelSpecFields, want) {
		t.Fatalf("spec fields %v do not match ModelInfo %v", modelSpecFields, want)
	}
}

func TestOverlayCoversEverySpecField(t *testing.T) {
	var src models.ModelInfo
	v := reflect.ValueOf(&src).Elem()
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		switch f.Kind() {
		case reflect.String:
			f.SetString("s")
		case reflect.Int, reflect.Int64:
			f.SetInt(7)
		case reflect.Slice:
			f.Set(reflect.MakeSlice(f.Type(), 1, 1))
		case reflect.Ptr:
			f.Set(reflect.New(f.Type().Elem()))
		default:
			t.Fatalf("unhandled ModelInfo kind %v: overlay needs a case", f.Kind())
		}
	}
	dst := models.ModelInfo{Name: "row"}
	overlayModelInfo(&dst, src)
	src.Name = "row"
	if !reflect.DeepEqual(dst, src) {
		t.Fatalf("overlay dropped fields: got %+v want %+v", dst, src)
	}
}

func TestRollbackRestoresModelSpecs(t *testing.T) {
	svc := setupService(t)
	rec, err := svc.Install([]byte(specPluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install v1: %v", err)
	}
	v2 := strings.Replace(specPluginSource, "@version 1.0.0", "@version 2.0.0", 1)
	v2 = strings.Replace(v2, "context_window = 200000", "context_window = 300000", 1)
	if _, err := svc.Install([]byte(v2), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install v2: %v", err)
	}
	infos, err := svc.GetModelInfos(t.Context(), "spec-provider", "spec-type", nil)
	if err != nil || infos[0].ContextWindow != 300000 {
		t.Fatalf("v2 specs: %+v %v", infos, err)
	}
	rolled, err := svc.Rollback(rec.ID)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if got := rolled.ModelSpecs["spec-type"]["m-one"].ContextWindow; got != 200000 {
		t.Fatalf("rolled-back specs: got %d", got)
	}
	infos, err = svc.GetModelInfos(t.Context(), "spec-provider", "spec-type", nil)
	if err != nil || infos[0].ContextWindow != 200000 {
		t.Fatalf("specs after rollback: %+v %v", infos, err)
	}
}

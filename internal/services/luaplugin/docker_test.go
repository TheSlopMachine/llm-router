package luaplugin

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/dockerhost"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

const dockerManifest = `--- @plugin Docker Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.8
--- @allow_host registry.npmjs.org
--- @require_api docker
--- @docker.image node:20-alpine
--- @docker.memory_limit_mb 256
--- @docker.cpu_limit 0.5
`

func TestParseManifestDockerDirectives(t *testing.T) {
	manifest, err := ParseManifest([]byte(dockerManifest + "\nreturn 1"))
	if err != nil {
		t.Fatalf("ParseManifest = %v", err)
	}
	policy, declared := manifest.DockerPolicy("tester/Docker-Plugin")
	if !declared || policy.Directives.MemoryLimitMB != 256 || policy.Directives.CPULimit != 0.5 || len(policy.Directives.Images) != 1 {
		t.Fatalf("policy = %+v, declared %v", policy, declared)
	}
	if policy.AllowHosts[0] != "registry.npmjs.org" {
		t.Errorf("policy allow hosts = %v", policy.AllowHosts)
	}
	failures := map[string]string{
		"docker tags without require_api": strings.Replace(dockerManifest, "--- @require_api docker\n", "", 1),
		"unknown api":                     strings.Replace(dockerManifest, "@require_api docker", "@require_api gpu", 1),
		"missing memory":                  strings.Replace(dockerManifest, "--- @docker.memory_limit_mb 256\n", "", 1),
		"unknown docker tag":              dockerManifest + "--- @docker.privileged true\n",
		"duplicate require_api":           dockerManifest + "--- @require_api docker\n",
	}
	for name, source := range failures {
		if _, err := ParseManifest([]byte(source + "\nreturn 1")); err == nil {
			t.Errorf("%s: ParseManifest succeeded", name)
		}
	}
}

const dockerPluginBody = `
local docker = llm_router.docker
llm_router.register("docker-type", {
  complete = function(ctx, request)
    local text = request.messages[1].content
    if text == "ensure-undeclared" then
      docker.ensure("web", { image = "redis:7" })
    end
    local _, fault = docker.status("web")
    local client = docker.open_http("web", 80)
    local _, request_fault = client:request({ method = "GET", url = "/health" })
    return { choices = { { message = { role = "assistant", content = fault.reason .. "," .. tostring(fault.docker) .. "," .. request_fault.reason } } } }, nil
  end,
})
`

func callDockerPlugin(t *testing.T, service *Service, text string) (string, error) {
	t.Helper()
	response, err := service.Complete(context.Background(), testMetadata("docker-type", nil, "docker-type/model", nil), &models.ChatCompletionRequest{
		Model:    "docker-type/model",
		Messages: []models.ChatMessage{{Role: "user", Content: text}},
	})
	if err != nil {
		return "", err
	}
	return response.Choices[0].Message.Content, nil
}

func TestPermissionsDigestTracksHostsAndDirectives(t *testing.T) {
	digest := func(source string) string {
		t.Helper()
		manifest, err := ParseManifest([]byte(source + "\nreturn 1"))
		if err != nil {
			t.Fatalf("ParseManifest = %v", err)
		}
		return manifest.Permissions().Digest
	}
	base := digest(dockerManifest)
	if digest(strings.Replace(dockerManifest, "@version 1.0.0", "@version 1.0.1", 1)) != base {
		t.Error("digest changed with the version only")
	}
	if digest(dockerManifest+"--- @allow_host github.com\n") == base {
		t.Error("digest ignores allow hosts")
	}
	if digest(strings.Replace(dockerManifest, "memory_limit_mb 256", "memory_limit_mb 512", 1)) == base {
		t.Error("digest ignores docker limits")
	}
	if err := CheckApproval([]byte(dockerManifest+"\nreturn 1"), base); err != nil {
		t.Errorf("CheckApproval with the matching digest = %v", err)
	}
	if err := CheckApproval([]byte(dockerManifest+"--- @allow_host github.com\n\nreturn 1"), base); !errors.Is(err, ErrPermissionsChanged) {
		t.Errorf("CheckApproval with other hosts = %v, want ErrPermissionsChanged", err)
	}
}

func TestDockerPluginWaitsForDockerHost(t *testing.T) {
	ctx := context.Background()
	service := setupService(t)
	docker := dockerhost.New(testutil.SetupTestSQLite(t))
	service.SetDocker(docker)
	installed, err := service.InstallSQLite(ctx, []byte(dockerManifest+dockerPluginBody), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if !installed.Disabled {
		t.Fatal("plugin installed enabled without a Docker host")
	}
	if _, err := service.SetDisabled(ctx, installed.ID, false); !errors.Is(err, ErrDockerUnconfigured) {
		t.Fatalf("enable without a Docker host = %v, want ErrDockerUnconfigured", err)
	}

	if _, err := docker.PutConnection(ctx, dockerhost.ConnectionUpdate{Endpoint: "tcp://127.0.0.1:1"}); err != nil {
		t.Fatalf("store connection: %v", err)
	}
	if _, err := service.SetDisabled(ctx, installed.ID, false); err != nil {
		t.Fatalf("enable with a Docker host: %v", err)
	}
	content, err := callDockerPlugin(t, service, "status")
	if err != nil {
		t.Fatalf("call with an unreachable daemon: %v", err)
	}
	if content != "unreachable,true,unreachable" {
		t.Errorf("with an unreachable daemon: %q", content)
	}
	_, err = callDockerPlugin(t, service, "ensure-undeclared")
	if err == nil || !strings.Contains(err.Error(), "not declared with @docker.image") {
		t.Errorf("undeclared image error = %v, want a raised spec error", err)
	}

	if _, err := docker.PutConnection(ctx, dockerhost.ConnectionUpdate{}); err != nil {
		t.Fatalf("remove connection: %v", err)
	}
	stored, err := service.GetSQLite(ctx, installed.ID)
	if err != nil {
		t.Fatalf("read plugin: %v", err)
	}
	if !stored.Disabled {
		t.Error("removing the Docker host left the plugin enabled")
	}
}

// Docker calls outside lifecycle hooks wait for the plugin's pending
// teardown, so none observes objects still being removed.
func TestDockerCallsWaitForTeardown(t *testing.T) {
	ctx := context.Background()
	service := setupService(t)
	docker := dockerhost.New(testutil.SetupTestSQLite(t))
	service.SetDocker(docker)
	if _, err := docker.PutConnection(ctx, dockerhost.ConnectionUpdate{Endpoint: "tcp://127.0.0.1:1"}); err != nil {
		t.Fatalf("store connection: %v", err)
	}
	installed, err := service.InstallSQLite(ctx, []byte(dockerManifest+dockerPluginBody), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	release := blockTeardown(service, installed.ID)
	type outcome struct {
		content string
		err     error
	}
	finished := make(chan outcome, 1)
	go func() {
		content, err := callDockerPlugin(t, service, "status")
		finished <- outcome{content, err}
	}()
	select {
	case result := <-finished:
		t.Fatalf("docker call ran during a teardown: %+v", result)
	case <-time.After(100 * time.Millisecond):
	}
	release()
	select {
	case result := <-finished:
		if result.err != nil || result.content != "unreachable,true,unreachable" {
			t.Errorf("docker call after the teardown = %q, %v", result.content, result.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("docker call never ran after the teardown")
	}
}

func TestDockerTableAbsentWithoutRequireAPI(t *testing.T) {
	state := newSandboxState(&executionContext{})
	defer state.Close()
	if err := state.DoString(`assert(llm_router.docker == nil)`); err != nil {
		t.Fatalf("docker table present without @require_api docker: %v", err)
	}
}

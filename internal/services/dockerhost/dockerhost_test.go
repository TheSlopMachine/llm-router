package dockerhost

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/dockerengine"
)

func testPolicy(t *testing.T) Policy {
	t.Helper()
	var directives Directives
	for _, line := range [][2]string{
		{"image", "node:20-alpine"},
		{"image", "nginx:1.27-alpine"},
		{"build", "mock https://codeload.github.com/owner/repository/tar.gz/abc123"},
		{"memory_limit_mb", "512"},
		{"cpu_limit", "1.5"},
		{"gpu_limit", "1"},
	} {
		if err := directives.Apply(line[0], line[1]); err != nil {
			t.Fatalf("Apply(%s %s) = %v", line[0], line[1], err)
		}
	}
	if err := directives.Finish(); err != nil {
		t.Fatalf("Finish = %v", err)
	}
	return Policy{PluginID: "example/mock", Directives: directives, AllowHosts: []string{"registry.npmjs.org"}}
}

func TestDirectives(t *testing.T) {
	policy := testPolicy(t)
	if policy.Directives.PidsLimit != defaultPidsLimit || policy.Directives.ContainerLimit != defaultContainerLimit || policy.Directives.ShmLimitMB != defaultShmLimitMB {
		t.Errorf("defaults not applied: %+v", policy.Directives)
	}
	if policy.Directives.Builds["mock"].Dockerfile != defaultDockerfile {
		t.Errorf("build dockerfile default = %q", policy.Directives.Builds["mock"].Dockerfile)
	}
	failures := [][2]string{
		{"memory_limit_mb", "8"},
		{"cpu_limit", "100"},
		{"gpu_limit", "-1"},
		{"build", "mock http://insecure.example.com/context.tar"},
		{"build", "Bad https://example.com/context.tar"},
		{"build", "other https://example.com/context.tar ../Dockerfile"},
		{"image", "has space"},
		{"unknown", "1"},
	}
	for _, line := range failures {
		var directives Directives
		if err := directives.Apply(line[0], line[1]); err == nil {
			t.Errorf("Apply(%s %s) succeeded, want error", line[0], line[1])
		}
	}
	var duplicate Directives
	_ = duplicate.Apply("memory_limit_mb", "64")
	if err := duplicate.Apply("memory_limit_mb", "64"); err == nil {
		t.Error("duplicate memory_limit_mb accepted")
	}
	var buildOnly Directives
	for _, line := range [][2]string{{"build", "mock https://example.com/context.tar"}, {"memory_limit_mb", "64"}, {"cpu_limit", "1"}} {
		_ = buildOnly.Apply(line[0], line[1])
	}
	if err := buildOnly.Finish(); err != nil || buildOnly.Images == nil {
		t.Errorf("build-only directives: images %v, error %v; want an empty list", buildOnly.Images, err)
	}
	var missing Directives
	_ = missing.Apply("image", "nginx")
	if err := missing.Finish(); err == nil || !strings.Contains(err.Error(), "memory_limit_mb") {
		t.Errorf("Finish without memory limit = %v", err)
	}
}

func TestValidateContainer(t *testing.T) {
	policy := testPolicy(t)
	valid := ContainerSpec{Image: "node:20-alpine", Ports: []int{8080, 80}, Volumes: map[string]string{"data": "/data"}, Files: map[string]string{"/app/config.yaml": "a: 1"}}
	if err := validateContainer(&valid, policy, "mock"); err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}
	if valid.MemoryLimitMB != 512 || valid.CPULimit != 1.5 || valid.Restart != RestartNo || !reflect.DeepEqual(valid.Ports, []int{80, 8080}) {
		t.Errorf("defaults not applied: %+v", valid)
	}
	cases := map[string]ContainerSpec{
		"undeclared image": {Image: "redis:7"},
		"image and build":  {Image: "node:20-alpine", Build: "mock"},
		"unknown build":    {Build: "other"},
		"host path":        {Image: "node:20-alpine", Volumes: map[string]string{"/srv": "/data"}},
		"memory ceiling":   {Image: "node:20-alpine", MemoryLimitMB: 1024},
		"gpu ceiling":      {Image: "node:20-alpine", GPUs: 2},
		"reserved env":     {Image: "node:20-alpine", Environment: map[string]string{"HTTPS_PROXY": "x"}},
		"relative file":    {Image: "node:20-alpine", Files: map[string]string{"config.yaml": "x"}},
		"bad port":         {Image: "node:20-alpine", Ports: []int{70000}},
		"bad restart":      {Image: "node:20-alpine", Restart: "always"},
	}
	for name, spec := range cases {
		err := validateContainer(&spec, policy, "mock")
		var specErr *SpecError
		if !errors.As(err, &specErr) {
			t.Errorf("%s: error = %v, want SpecError", name, err)
		}
	}
}

func TestParseCompose(t *testing.T) {
	policy := testPolicy(t)
	text := `
version: "3.8"
x-common: &common
  restart: "no"
services:
  api:
    build:
      context: https://codeload.github.com/owner/repository/tar.gz/abc123
    expose: ["3000"]
    environment:
      MODE: ${MODE:-mock}
      TOKEN: "$$literal"
    depends_on:
      cache:
        condition: service_healthy
    deploy:
      resources:
        limits: { memory: 256m, cpus: "0.5" }
        reservations:
          devices:
            - driver: nvidia
              count: 1
              capabilities: [gpu]
  cache:
    image: nginx:1.27-alpine
    command: nginx -g 'daemon off;'
    healthcheck:
      test: ["CMD-SHELL", "wget -q -O- http://127.0.0.1/"]
      interval: 5s
      retries: 3
    volumes:
      - data:/var/cache/nginx
volumes:
  data: {}
`
	project, err := ParseCompose(text, map[string]string{}, policy)
	if err != nil {
		t.Fatalf("ParseCompose = %v", err)
	}
	api := project.Services["api"]
	if api.Build != "mock" || !reflect.DeepEqual(api.Ports, []int{3000}) || api.Environment["MODE"] != "mock" || api.Environment["TOKEN"] != "$literal" {
		t.Errorf("api = %+v", api)
	}
	if api.MemoryLimitMB != 256 || api.CPULimit != 0.5 || api.GPUs != 1 || api.DependsOn["cache"] != ConditionHealthy {
		t.Errorf("api limits = %+v", api)
	}
	cache := project.Services["cache"]
	if !reflect.DeepEqual(cache.Command, []string{"nginx", "-g", "daemon off;"}) || cache.Health == nil || cache.Health.IntervalSeconds != 5 || cache.Volumes["data"] != "/var/cache/nginx" {
		t.Errorf("cache = %+v", cache)
	}
	if !reflect.DeepEqual(cache.Health.Command, []string{"/bin/sh", "-c", "wget -q -O- http://127.0.0.1/"}) {
		t.Errorf("health command = %q", cache.Health.Command)
	}
	if err := validateProject(&project, policy); err != nil {
		t.Fatalf("validateProject = %v", err)
	}
	if order := project.ServiceNames(); !reflect.DeepEqual(order, []string{"cache", "api"}) {
		t.Errorf("start order = %v", order)
	}

	rejected := map[string]string{
		"publish":    "services:\n  a:\n    image: nginx:1.27-alpine\n    ports: [\"80:80\"]\n",
		"bind mount": "services:\n  a:\n    image: nginx:1.27-alpine\n    volumes: [\"/srv:/data\"]\n",
		"privileged": "services:\n  a:\n    image: nginx:1.27-alpine\n    privileged: true\n",
		"variable":   "services:\n  a:\n    image: ${IMAGE}\n",
		"build url":  "services:\n  a:\n    build: https://example.com/other.tar\n",
		"undeclared": "services:\n  a:\n    image: nginx:1.27-alpine\n    volumes: [\"data:/data\"]\n",
	}
	for name, document := range rejected {
		_, err := ParseCompose(document, nil, policy)
		var specErr *SpecError
		if !errors.As(err, &specErr) {
			t.Errorf("%s: error = %v, want SpecError", name, err)
		}
	}
	if _, err := ParseCompose("services:\n  a:\n    ports: [1]\n", nil, policy); err == nil || !strings.Contains(err.Error(), "services.a.ports") {
		t.Errorf("error lacks the key path: %v", err)
	}
}

func TestInterpolateText(t *testing.T) {
	variables := map[string]string{"SET": "value", "EMPTY": ""}
	cases := map[string]string{
		"plain":            "plain",
		"$SET":             "value",
		"${SET}":           "value",
		"${MISSING:-x}":    "x",
		"${EMPTY:-x}":      "x",
		"${EMPTY-x}":       "",
		"a$${SET}b":        "a${SET}b",
		"${SET}-${SET}":    "value-value",
		"prefix_${SET}_ok": "prefix_value_ok",
	}
	for input, want := range cases {
		got, err := interpolateText(input, variables)
		if err != nil || got != want {
			t.Errorf("interpolateText(%q) = %q, %v, want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"${MISSING}", "${EMPTY:?needed}", "$", "${unclosed"} {
		if _, err := interpolateText(input, variables); err == nil {
			t.Errorf("interpolateText(%q) succeeded, want error", input)
		}
	}
}

func TestNamesAreDistinctAndValid(t *testing.T) {
	current := &session{namespace: DefaultNamespace}
	first := current.containerName("a.b/c", target{key: "nginx"})
	second := current.containerName("a/b.c", target{key: "nginx"})
	if first == second {
		t.Fatalf("names collide: %s", first)
	}
	if !strings.HasPrefix(first, "llm-router.a.b.c.nginx-") {
		t.Errorf("name %q lacks the readable prefix", first)
	}
	tag := current.buildTag("Some Author/My Plugin", "mock", BuildSource{URL: "https://example.com/a.tar", Dockerfile: "Dockerfile"})
	repository, version := dockerengine.SplitReference(tag)
	if repository != "llmr-build/llm.router.some.author.my.plugin.mock" || len(version) != 12 {
		t.Errorf("build tag = %q", tag)
	}
	for _, text := range []string{"nginx", "project/api"} {
		if _, err := parseTarget(text); err != nil {
			t.Errorf("parseTarget(%q) = %v", text, err)
		}
	}
	for _, text := range []string{"", "Nginx", "a/b/c", "a/", "../x"} {
		if _, err := parseTarget(text); err == nil {
			t.Errorf("parseTarget(%q) succeeded", text)
		}
	}
}

func writeArchive(t *testing.T, compressed bool, entries map[string]string) string {
	t.Helper()
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	for _, name := range sortedKeys(entries) {
		content := entries[name]
		header := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if strings.HasSuffix(name, "/") {
			header = &tar.Header{Name: name, Mode: 0o755, Typeflag: tar.TypeDir}
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeReg {
			_, _ = writer.Write([]byte(content))
		}
	}
	writer.Close()
	data := buffer.Bytes()
	if compressed {
		var gzipped bytes.Buffer
		gzipWriter := gzip.NewWriter(&gzipped)
		_, _ = gzipWriter.Write(data)
		gzipWriter.Close()
		data = gzipped.Bytes()
	}
	path := filepath.Join(t.TempDir(), "context")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func archiveNames(t *testing.T, data []byte) []string {
	t.Helper()
	reader := tar.NewReader(bytes.NewReader(data))
	var names []string
	for {
		header, err := reader.Next()
		if err != nil {
			break
		}
		names = append(names, header.Name)
	}
	return names
}

func TestBuildContextStripsSingleTopDirectory(t *testing.T) {
	nested := writeArchive(t, true, map[string]string{"repository-abc123/": "", "repository-abc123/Dockerfile": "FROM node", "repository-abc123/src/server.js": "x"})
	prefix, err := contextPrefix(nested)
	if err != nil || prefix != "repository-abc123/" {
		t.Fatalf("contextPrefix = %q, %v", prefix, err)
	}
	var output bytes.Buffer
	if err := rewriteContext(nested, prefix, &output); err != nil {
		t.Fatalf("rewriteContext = %v", err)
	}
	if names := archiveNames(t, output.Bytes()); !reflect.DeepEqual(names, []string{"Dockerfile", "src/server.js"}) {
		t.Errorf("rewritten names = %v", names)
	}
	flat := writeArchive(t, false, map[string]string{"Dockerfile": "FROM node", "src/server.js": "x"})
	if prefix, err := contextPrefix(flat); err != nil || prefix != "" {
		t.Errorf("flat contextPrefix = %q, %v", prefix, err)
	}
	escaping := writeArchive(t, false, map[string]string{"Dockerfile": "x", "../evil": "x"})
	if err := rewriteContext(escaping, "", &bytes.Buffer{}); err == nil {
		t.Error("rewriteContext accepted an entry leaving the context")
	}
}

func TestResolveDialMode(t *testing.T) {
	ssh, _ := dockerengine.ParseEndpoint("ssh://user@host")
	tcp, _ := dockerengine.ParseEndpoint("tcp://127.0.0.1:2375")
	if mode := resolveDialMode(DialModeAuto, ssh, dockerengine.SystemInfo{}); mode != DialModeRoute {
		t.Errorf("ssh auto = %s", mode)
	}
	if mode := resolveDialMode(DialModeAuto, tcp, dockerengine.SystemInfo{}); mode != DialModeRelay {
		t.Errorf("tcp auto = %s", mode)
	}
	rootless := dockerengine.SystemInfo{SecurityOptions: []string{"name=seccomp", "name=rootless"}}
	if mode := resolveDialMode(DialModeAuto, ssh, rootless); mode != DialModeRelay {
		t.Errorf("rootless auto = %s", mode)
	}
	if mode := resolveDialMode(DialModeRoute, tcp, rootless); mode != DialModeRoute {
		t.Errorf("explicit route = %s", mode)
	}
}

// A transport failure on one call must not close the session other calls
// hold: a connection test racing a failed poll used to lose its session.
func TestUnavailableFaultKeepsSession(t *testing.T) {
	live := &session{}
	service := &Service{current: live, transports: map[transportKey]*http.Transport{}}
	err := service.faultFromError(fmt.Errorf("%w: %w", dockerengine.ErrUnavailable, errors.New("connection reset")), "list containers")
	fault, ok := AsFault(err)
	if !ok || fault.Reason != ReasonUnreachable {
		t.Fatalf("faultFromError = %v, want an unreachable fault", err)
	}
	if service.current != live {
		t.Error("faultFromError dropped the live session")
	}
}

// A plugin-wide teardown waits for running target operations and holds new
// ones until it ends. The gateway lock taken under a shared hold never
// deadlocks behind a waiting teardown.
func TestPluginLockOrdersTeardownAndTargets(t *testing.T) {
	const settle = 50 * time.Millisecond
	service := &Service{}
	unlockTarget := service.lock("plugin", "web")

	teardownHeld := make(chan struct{})
	releaseTeardown := make(chan struct{})
	go func() {
		unlock := service.lockPlugin("plugin")
		close(teardownHeld)
		<-releaseTeardown
		unlock()
	}()
	select {
	case <-teardownHeld:
		t.Fatal("teardown overlapped a running target operation")
	case <-time.After(settle):
	}

	gateway := service.targetLock("plugin", "\x00gateway")
	gatewayTaken := make(chan struct{})
	go func() {
		gateway.Lock()
		close(gatewayTaken)
		gateway.Unlock()
	}()
	select {
	case <-gatewayTaken:
	case <-time.After(5 * time.Second):
		t.Fatal("gateway lock blocked behind a waiting teardown")
	}

	unlockOther := service.lock("other", "web")
	unlockOther()

	unlockTarget()
	select {
	case <-teardownHeld:
	case <-time.After(5 * time.Second):
		t.Fatal("teardown never started after the target operation ended")
	}

	targetHeld := make(chan struct{})
	go func() {
		unlock := service.lock("plugin", "web")
		close(targetHeld)
		unlock()
	}()
	select {
	case <-targetHeld:
		t.Fatal("target operation ran during the teardown")
	case <-time.After(settle):
	}
	close(releaseTeardown)
	select {
	case <-targetHeld:
	case <-time.After(5 * time.Second):
		t.Fatal("target operation never ran after the teardown")
	}
}

func TestRecordProgress(t *testing.T) {
	service := &Service{images: map[string]*imageTask{}}
	task := &imageTask{progress: ImageProgress{Phase: PhasePull}}
	layer := func(id, status string, current, total int64) dockerengine.ProgressEvent {
		event := dockerengine.ProgressEvent{LayerID: id, Status: status}
		event.ProgressDetail.Current, event.ProgressDetail.Total = current, total
		return event
	}
	service.recordProgress(task, layer("a", "Downloading", 10, 100))
	service.recordProgress(task, layer("b", "Downloading", 5, 50))
	service.recordProgress(task, layer("a", "Pull complete", 0, 0))
	if task.progress.BytesDone != 105 || task.progress.BytesTotal != 150 || task.progress.Detail != "Pull complete" {
		t.Errorf("pull progress = %+v", task.progress)
	}
	build := &imageTask{progress: ImageProgress{Phase: PhaseBuild}}
	service.recordProgress(build, dockerengine.ProgressEvent{Stream: "Step 4/9 : RUN npm install --only=production\n"})
	service.recordProgress(build, dockerengine.ProgressEvent{Stream: " ---> Running in 1234\n"})
	if build.progress.Step != 4 || build.progress.Steps != 9 || build.progress.Detail != "RUN npm install --only=production" {
		t.Errorf("build progress = %+v", build.progress)
	}
}

func TestRegistryHost(t *testing.T) {
	cases := map[string]string{
		"nginx":                   "docker.io",
		"library/nginx:1":         "docker.io",
		"ghcr.io/owner/image:tag": "ghcr.io",
		"registry:5000/image":     "registry:5000",
		"localhost/image":         "localhost",
		"Example.COM/owner/image": "example.com",
	}
	for reference, want := range cases {
		if got := registryHost(reference); got != want {
			t.Errorf("registryHost(%q) = %q, want %q", reference, got, want)
		}
	}
}

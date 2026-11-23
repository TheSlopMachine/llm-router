package dockerhost

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/dockerengine"
	"github.com/TheSlopMachine/llm-router/internal/netguard"
)

//go:embed payload/*
var payloadFiles embed.FS

// Image preparation bounds.
const (
	// imageTaskTimeout bounds one pull or build.
	imageTaskTimeout = 30 * time.Minute
	// imageFailureHold keeps a failed task's error before a retry.
	imageFailureHold = time.Minute
	// maximumBuildContextBytes caps a downloaded build context archive.
	maximumBuildContextBytes = 512 << 20
	// buildDownloadTimeout bounds the build context download.
	buildDownloadTimeout = 10 * time.Minute
)

// imageTask is one pull or build in flight or finished. progress is
// guarded by Service.imagesMu.
type imageTask struct {
	done     chan struct{}
	err      error
	finished time.Time
	progress ImageProgress
	layers   map[string][2]int64
}

// Image preparation phases.
const (
	PhaseDownloadContext = "download_context"
	PhasePull            = "pull"
	PhaseBuild           = "build"
)

// ImageProgress is the live state of one pull or build.
type ImageProgress struct {
	Phase      string    `json:"phase"`
	Step       int       `json:"step,omitempty"`
	Steps      int       `json:"steps,omitempty"`
	Detail     string    `json:"detail,omitempty"`
	BytesDone  int64     `json:"bytes_done,omitempty"`
	BytesTotal int64     `json:"bytes_total,omitempty"`
	StartedAt  time.Time `json:"started_at"`
}

// buildStepPattern matches the step lines of the classic builder.
var buildStepPattern = regexp.MustCompile(`^Step (\d+)/(\d+) : (.+)$`)

// maximumProgressDetail caps the instruction text kept per step.
const maximumProgressDetail = 160

// recordProgress folds one progress line into the task: build steps from
// output lines, bytes from layer progress (pulls and base image pulls of
// builds).
func (service *Service) recordProgress(task *imageTask, event dockerengine.ProgressEvent) {
	service.imagesMu.Lock()
	defer service.imagesMu.Unlock()
	if match := buildStepPattern.FindStringSubmatch(strings.TrimSpace(event.Stream)); match != nil {
		task.progress.Phase = PhaseBuild
		task.progress.Step, _ = strconv.Atoi(match[1])
		task.progress.Steps, _ = strconv.Atoi(match[2])
		detail := match[3]
		if len(detail) > maximumProgressDetail {
			detail = detail[:maximumProgressDetail]
		}
		task.progress.Detail = detail
		return
	}
	if event.LayerID == "" {
		return
	}
	if task.layers == nil {
		task.layers = map[string][2]int64{}
	}
	switch {
	case event.ProgressDetail.Total > 0:
		task.layers[event.LayerID] = [2]int64{event.ProgressDetail.Current, event.ProgressDetail.Total}
	case event.Status == "Pull complete" || event.Status == "Already exists" || event.Status == "Download complete":
		if layer, known := task.layers[event.LayerID]; known {
			task.layers[event.LayerID] = [2]int64{layer[1], layer[1]}
		}
	}
	var done, total int64
	for _, layer := range task.layers {
		done += layer[0]
		total += layer[1]
	}
	task.progress.BytesDone, task.progress.BytesTotal = done, total
	if task.progress.Phase == PhasePull {
		task.progress.Detail = event.Status
	}
}

// progressReader counts bytes read into the task progress.
type progressReader struct {
	service *Service
	task    *imageTask
	source  io.Reader
}

func (reader progressReader) Read(buffer []byte) (int, error) {
	n, err := reader.source.Read(buffer)
	if n > 0 {
		reader.service.imagesMu.Lock()
		reader.task.progress.BytesDone += int64(n)
		reader.service.imagesMu.Unlock()
	}
	return n, err
}

// ImageStatus is the preparation state of one declared image.
type ImageStatus struct {
	Reference string         `json:"reference"`
	Build     string         `json:"build,omitempty"`
	State     string         `json:"state"`
	Progress  *ImageProgress `json:"progress,omitempty"`
	Error     string         `json:"error,omitempty"`
}

// Image states.
const (
	ImageReady     = "ready"
	ImagePreparing = "preparing"
	ImageFailed    = "failed"
	ImageAbsent    = "absent"
)

// imageStatuses reports every image a policy declares.
func (service *Service) imageStatuses(ctx context.Context, current *session, policy Policy) ([]ImageStatus, error) {
	requests := make([]imageRequest, 0, len(policy.Directives.Images)+len(policy.Directives.Builds))
	for _, image := range policy.Directives.Images {
		requests = append(requests, imageRequest{reference: image, pluginID: policy.PluginID})
	}
	for _, name := range sortedKeys(policy.Directives.Builds) {
		requests = append(requests, current.resolveImage(policy, ContainerSpec{Build: name}))
	}
	statuses := make([]ImageStatus, 0, len(requests))
	for _, request := range requests {
		status := ImageStatus{Reference: request.reference, Build: request.build, State: ImageAbsent}
		if _, err := current.client.ImageInspect(ctx, request.reference); err == nil {
			status.State = ImageReady
			statuses = append(statuses, status)
			continue
		} else if !errors.Is(err, dockerengine.ErrNotFound) {
			return nil, service.faultFromError(err, "inspect image %s", request.reference)
		}
		service.imagesMu.Lock()
		if task, ok := service.images[request.reference]; ok {
			select {
			case <-task.done:
				if task.err != nil {
					status.State = ImageFailed
					status.Error = task.err.Error()
				}
			default:
				progress := task.progress
				status.State = ImagePreparing
				status.Progress = &progress
			}
		}
		service.imagesMu.Unlock()
		statuses = append(statuses, status)
	}
	return statuses, nil
}

// imageRequest names one image to prepare.
type imageRequest struct {
	reference string
	pluginID  string
	build     string
	source    BuildSource
}

// resolveImage returns the image reference a spec runs.
func (session *session) resolveImage(policy Policy, spec ContainerSpec) imageRequest {
	if spec.Build != "" {
		source := policy.Directives.Builds[spec.Build]
		return imageRequest{reference: session.buildTag(policy.PluginID, spec.Build, source), pluginID: policy.PluginID, build: spec.Build, source: source}
	}
	return imageRequest{reference: spec.Image, pluginID: policy.PluginID}
}

// ensureImage reports whether the image is present. A missing image starts
// a background pull or build and reports false. A recent failure returns
// its error until the hold expires.
func (service *Service) ensureImage(ctx context.Context, current *session, request imageRequest) (bool, error) {
	if _, err := current.client.ImageInspect(ctx, request.reference); err == nil {
		return true, nil
	} else if !errors.Is(err, dockerengine.ErrNotFound) {
		return false, service.faultFromError(err, "inspect image %s", request.reference)
	}
	service.imagesMu.Lock()
	defer service.imagesMu.Unlock()
	task, exists := service.images[request.reference]
	if exists {
		select {
		case <-task.done:
			if task.err == nil {
				// Finished, yet the inspect missed: removed since. Prepare again.
				break
			}
			if time.Since(task.finished) < imageFailureHold {
				return false, task.err
			}
		default:
			return false, nil
		}
	}
	phase := PhasePull
	if request.build != "" {
		phase = PhaseDownloadContext
	}
	task = &imageTask{done: make(chan struct{}), progress: ImageProgress{Phase: phase, StartedAt: time.Now().UTC()}}
	service.images[request.reference] = task
	go service.runImageTask(current, request, task)
	return false, nil
}

func (service *Service) runImageTask(current *session, request imageRequest, task *imageTask) {
	ctx, cancel := context.WithTimeout(context.Background(), imageTaskTimeout)
	defer cancel()
	started := time.Now()
	var err error
	onProgress := func(event dockerengine.ProgressEvent) { service.recordProgress(task, event) }
	if request.build != "" {
		service.logInfo("Docker image build started", "plugin_id", request.pluginID, "image", request.reference, "url", request.source.URL)
		err = service.buildImage(ctx, current, request, task)
	} else {
		service.logInfo("Docker image pull started", "plugin_id", request.pluginID, "image", request.reference)
		err = service.pullImage(ctx, current, request.reference, onProgress)
	}
	service.imagesMu.Lock()
	task.err = err
	task.finished = time.Now()
	close(task.done)
	service.imagesMu.Unlock()
	if err != nil {
		service.logWarn("Docker image preparation failed", "plugin_id", request.pluginID, "image", request.reference, "error", err)
		return
	}
	service.logInfo("Docker image ready", "plugin_id", request.pluginID, "image", request.reference, "latency_ms", time.Since(started).Milliseconds())
}

// PrepareImages starts preparing every image a policy declares. It runs
// detached; failures log.
func (service *Service) PrepareImages(policy Policy) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), sessionProbeTimeout)
		defer cancel()
		current, err := service.session(ctx)
		if err != nil {
			service.logDebug("Docker image preparation skipped", "plugin_id", policy.PluginID, "error", err)
			return
		}
		if _, err := service.ensureGatewayImage(ctx, current); err != nil {
			service.logWarn("Docker gateway image not ready", "plugin_id", policy.PluginID, "error", err)
		}
		for _, image := range policy.Directives.Images {
			if _, err := service.ensureImage(ctx, current, imageRequest{reference: image, pluginID: policy.PluginID}); err != nil {
				service.logWarn("Docker image preparation failed", "plugin_id", policy.PluginID, "image", image, "error", err)
			}
		}
		for _, name := range sortedKeys(policy.Directives.Builds) {
			request := current.resolveImage(policy, ContainerSpec{Build: name})
			if _, err := service.ensureImage(ctx, current, request); err != nil {
				service.logWarn("Docker image preparation failed", "plugin_id", policy.PluginID, "image", request.reference, "error", err)
			}
		}
	}()
}

// registryHost returns the registry an image reference pulls from.
func registryHost(reference string) string {
	first, _, found := strings.Cut(reference, "/")
	if !found || (!strings.ContainsAny(first, ".:") && first != "localhost") {
		return "docker.io"
	}
	return strings.ToLower(first)
}

// registryServerAddress is the address Docker expects in auth payloads.
func registryServerAddress(host string) string {
	if host == "docker.io" {
		return "https://index.docker.io/v1/"
	}
	return host
}

type registryAuth struct {
	Username      string `json:"username"`
	Password      string `json:"password"`
	ServerAddress string `json:"serveraddress"`
}

// pullImage pulls one reference with the stored login of its registry.
func (service *Service) pullImage(ctx context.Context, current *session, reference string, onProgress func(dockerengine.ProgressEvent)) error {
	logins, err := service.registryLogins(ctx)
	if err != nil {
		return err
	}
	header := ""
	host := registryHost(reference)
	if login, ok := logins[host]; ok {
		encoded, err := json.Marshal(registryAuth{Username: login.Username, Password: login.Secret, ServerAddress: registryServerAddress(host)})
		if err != nil {
			return err
		}
		header = base64.URLEncoding.EncodeToString(encoded)
	}
	if err := current.client.ImagePull(ctx, reference, header, onProgress); err != nil {
		var apiError *dockerengine.APIError
		if errors.As(err, &apiError) && (apiError.Status == http.StatusUnauthorized || apiError.Status == http.StatusForbidden) {
			return newFault(ReasonRegistryAuth, err, "pull %s: registry %s refused the login: %v", reference, host, err)
		}
		return service.faultFromError(err, "pull %s", reference)
	}
	return nil
}

// buildImage downloads the declared context archive, strips a single
// top-level directory and builds the image with the plugin limits.
func (service *Service) buildImage(ctx context.Context, current *session, request imageRequest, task *imageTask) error {
	archive, err := downloadContext(ctx, request.source.URL, func(total int64, body io.Reader) io.Reader {
		service.imagesMu.Lock()
		task.progress.BytesTotal = total
		service.imagesMu.Unlock()
		return progressReader{service: service, task: task, source: body}
	})
	if err != nil {
		return newFault(ReasonDaemonError, err, "download build context %s: %v", request.source.URL, err)
	}
	defer os.Remove(archive)
	prefix, err := contextPrefix(archive)
	if err != nil {
		return newFault(ReasonDaemonError, err, "read build context %s: %v", request.source.URL, err)
	}
	logins, err := service.registryLogins(ctx)
	if err != nil {
		return err
	}
	registryConfig := ""
	if len(logins) > 0 {
		configs := map[string]registryAuth{}
		for host, login := range logins {
			address := registryServerAddress(host)
			configs[address] = registryAuth{Username: login.Username, Password: login.Secret, ServerAddress: address}
		}
		encoded, err := json.Marshal(configs)
		if err != nil {
			return err
		}
		registryConfig = base64.URLEncoding.EncodeToString(encoded)
	}
	policy := service.policyLimits(request.pluginID)
	service.imagesMu.Lock()
	task.progress = ImageProgress{Phase: PhaseBuild, StartedAt: task.progress.StartedAt}
	service.imagesMu.Unlock()
	reader, writer := io.Pipe()
	go func() {
		writer.CloseWithError(rewriteContext(archive, prefix, writer))
	}()
	labels := current.ownerLabels(request.pluginID, roleImage)
	err = current.client.ImageBuild(ctx, reader, dockerengine.BuildOptions{
		Tag:            request.reference,
		Dockerfile:     request.source.Dockerfile,
		Labels:         labels,
		MemoryBytes:    int64(policy.MemoryLimitMB) << 20,
		NanoCPUs:       int64(policy.CPULimit * 1e9),
		RegistryConfig: registryConfig,
		Progress:       func(event dockerengine.ProgressEvent) { service.recordProgress(task, event) },
	})
	reader.Close()
	if err != nil {
		return service.faultFromError(err, "build %s from %s", request.reference, request.source.URL)
	}
	return nil
}

// policyLimits finds the directives of a plugin for build limits. A plugin
// without a policy builds with the smallest limits.
func (service *Service) policyLimits(pluginID string) Directives {
	service.wiringMu.RLock()
	source := service.policies
	service.wiringMu.RUnlock()
	fallback := Directives{MemoryLimitMB: minimumMemoryLimitMB, CPULimit: minimumCPULimit}
	if source == nil {
		return fallback
	}
	policies, err := source(context.Background())
	if err != nil {
		return fallback
	}
	for _, policy := range policies {
		if policy.PluginID == pluginID {
			return policy.Directives
		}
	}
	return fallback
}

// downloadContext fetches a build context archive from a public https URL
// into a temporary file. track wraps the body with the announced size
// (-1 when unknown) for progress counting.
func downloadContext(ctx context.Context, rawURL string, track func(total int64, body io.Reader) io.Reader) (string, error) {
	client := netguard.NewHTTPClient(buildDownloadTimeout)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", response.StatusCode)
	}
	file, err := os.CreateTemp("", "llm-router-build-context-*")
	if err != nil {
		return "", err
	}
	written, copyError := io.Copy(file, io.LimitReader(track(response.ContentLength, response.Body), maximumBuildContextBytes+1))
	closeError := file.Close()
	if copyError == nil && written > maximumBuildContextBytes {
		copyError = fmt.Errorf("archive exceeds %d bytes", maximumBuildContextBytes)
	}
	if copyError == nil {
		copyError = closeError
	}
	if copyError != nil {
		os.Remove(file.Name())
		return "", copyError
	}
	return file.Name(), nil
}

// openArchive opens a tar or tar.gz file.
func openArchive(file *os.File) (*tar.Reader, error) {
	buffered := bufio.NewReader(file)
	magic, err := buffered.Peek(2)
	if err != nil {
		return nil, fmt.Errorf("archive is empty or unreadable: %w", err)
	}
	if magic[0] == 0x1f && magic[1] == 0x8b {
		decompressed, err := gzip.NewReader(buffered)
		if err != nil {
			return nil, err
		}
		return tar.NewReader(decompressed), nil
	}
	return tar.NewReader(buffered), nil
}

// contextPrefix returns the single top-level directory every entry shares,
// or "" when entries sit at the root.
func contextPrefix(archivePath string) (string, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	reader, err := openArchive(file)
	if err != nil {
		return "", err
	}
	prefix := ""
	entries := 0
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		if header.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		name := strings.TrimPrefix(header.Name, "./")
		first, rest, nested := strings.Cut(name, "/")
		if !nested || (rest == "" && header.Typeflag != tar.TypeDir) {
			return "", nil
		}
		if entries == 0 {
			prefix = first
		} else if first != prefix {
			return "", nil
		}
		entries++
	}
	if entries == 0 {
		return "", errors.New("archive holds no files")
	}
	return prefix + "/", nil
}

// rewriteContext streams the archive as an uncompressed tar with prefix
// removed from every entry name.
func rewriteContext(archivePath, prefix string, output io.Writer) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	reader, err := openArchive(file)
	if err != nil {
		return err
	}
	writer := tar.NewWriter(output)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if header.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		name := strings.TrimPrefix(strings.TrimPrefix(header.Name, "./"), prefix)
		if name == "" {
			continue
		}
		cleaned := path.Clean(name)
		if strings.HasPrefix(cleaned, "../") || cleaned == ".." || path.IsAbs(cleaned) {
			return fmt.Errorf("archive entry %q leaves the context", header.Name)
		}
		header.Name = name
		if err := writer.WriteHeader(header); err != nil {
			return err
		}
		if _, err := io.Copy(writer, reader); err != nil {
			return err
		}
	}
	return writer.Close()
}

// gatewayPayload returns the gzipped gateway binary for one architecture.
func gatewayPayload(architecture string) ([]byte, error) {
	switch architecture {
	case "x86_64", "amd64":
		architecture = "amd64"
	case "aarch64", "arm64":
		architecture = "arm64"
	default:
		return nil, fmt.Errorf("docker host architecture %q has no gateway build: set a gateway image in Settings", architecture)
	}
	data, err := payloadFiles.ReadFile("payload/gateway-linux-" + architecture + ".gz")
	if err != nil {
		return nil, fmt.Errorf("gateway payload for linux/%s is missing from this build: run make init", architecture)
	}
	return data, nil
}

// ensureGatewayImage returns the gateway image reference, importing the
// embedded program when the image is missing. An operator override image
// is pulled instead.
func (service *Service) ensureGatewayImage(ctx context.Context, current *session) (string, error) {
	if current.gatewayImage != "" {
		ready, err := service.ensureImage(ctx, current, imageRequest{reference: current.gatewayImage})
		if err != nil {
			return "", err
		}
		if !ready {
			return "", newFault(ReasonPreparing, nil, "gateway image %s is downloading", current.gatewayImage)
		}
		return current.gatewayImage, nil
	}
	payload, err := gatewayPayload(current.info.Architecture)
	if err != nil {
		return "", newFault(ReasonUnconfigured, err, "%v", err)
	}
	sum := sha256.Sum256(payload)
	reference := "llmr-gateway:" + hex.EncodeToString(sum[:])[:12]
	if _, err := current.client.ImageInspect(ctx, reference); err == nil {
		return reference, nil
	} else if !errors.Is(err, dockerengine.ErrNotFound) {
		return "", service.faultFromError(err, "inspect gateway image")
	}
	tarball, err := gatewayRootfs(payload)
	if err != nil {
		return "", newFault(ReasonDaemonError, err, "pack gateway image: %v", err)
	}
	repository, tag := dockerengine.SplitReference(reference)
	changes := []string{
		`ENTRYPOINT ["` + gatewayBinaryPath + `"]`,
		`CMD ["proxy"]`,
		`LABEL ` + labelNamespace + `=` + current.namespace + ` ` + labelRole + `=` + roleImage,
	}
	if err := current.client.ImageImport(ctx, bytes.NewReader(tarball), repository, tag, changes); err != nil {
		return "", service.faultFromError(err, "import gateway image")
	}
	service.logInfo("Docker gateway image imported", "image", reference)
	return reference, nil
}

// gatewayRootfs packs the gateway binary as the only file of a root
// filesystem tarball.
func gatewayRootfs(payload []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	binary, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	header := &tar.Header{Name: strings.TrimPrefix(gatewayBinaryPath, "/"), Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg, ModTime: time.Unix(0, 0)}
	if err := writer.WriteHeader(header); err != nil {
		return nil, err
	}
	if _, err := writer.Write(binary); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// filesArchive packs spec files into one tar extracted at the root.
func filesArchive(files map[string]string) ([]byte, error) {
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	directories := map[string]bool{}
	for _, target := range sortedKeys(files) {
		name := strings.TrimPrefix(target, "/")
		for directory := path.Dir(name); directory != "." && !directories[directory]; directory = path.Dir(directory) {
			directories[directory] = true
		}
	}
	for _, directory := range sortedKeys(directories) {
		if err := writer.WriteHeader(&tar.Header{Name: directory + "/", Mode: 0o755, Typeflag: tar.TypeDir, ModTime: time.Unix(0, 0)}); err != nil {
			return nil, err
		}
	}
	for _, target := range sortedKeys(files) {
		content := files[target]
		header := &tar.Header{Name: strings.TrimPrefix(target, "/"), Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg, ModTime: time.Unix(0, 0)}
		if err := writer.WriteHeader(header); err != nil {
			return nil, err
		}
		if _, err := io.WriteString(writer, content); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

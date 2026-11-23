package dockerengine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// VersionInfo is the GET /version answer.
type VersionInfo struct {
	Version       string `json:"Version"`
	APIVersion    string `json:"ApiVersion"`
	MinAPIVersion string `json:"MinAPIVersion"`
	OS            string `json:"Os"`
	Architecture  string `json:"Arch"`
}

// SystemInfo is the subset of GET /info the router reads.
type SystemInfo struct {
	OperatingSystem string                     `json:"OperatingSystem"`
	OSType          string                     `json:"OSType"`
	Architecture    string                     `json:"Architecture"`
	SecurityOptions []string                   `json:"SecurityOptions"`
	Runtimes        map[string]json.RawMessage `json:"Runtimes"`
	CDISpecDirs     []string                   `json:"CDISpecDirs"`
	CPUCount        int                        `json:"NCPU"`
	MemoryTotal     int64                      `json:"MemTotal"`
}

// Rootless reports a daemon running in rootless mode.
func (info SystemInfo) Rootless() bool {
	for _, option := range info.SecurityOptions {
		if strings.Contains(option, "name=rootless") {
			return true
		}
	}
	return false
}

// GPUCapable reports an NVIDIA runtime or CDI device specifications.
func (info SystemInfo) GPUCapable() bool {
	if _, ok := info.Runtimes["nvidia"]; ok {
		return true
	}
	return len(info.CDISpecDirs) > 0
}

// Ping checks the daemon answers.
func (client *Client) Ping(ctx context.Context) error {
	response, err := client.do(ctx, request{method: http.MethodGet, path: "/_ping"})
	if err != nil {
		return err
	}
	response.Body.Close()
	return nil
}

// Version reads the daemon version.
func (client *Client) Version(ctx context.Context) (VersionInfo, error) {
	var info VersionInfo
	_, err := client.doJSON(ctx, http.MethodGet, "/version", nil, nil, &info)
	return info, err
}

// Info reads daemon system information.
func (client *Client) Info(ctx context.Context) (SystemInfo, error) {
	var info SystemInfo
	_, err := client.doJSON(ctx, http.MethodGet, "/info", nil, nil, &info)
	return info, err
}

// ImageSummary is the subset of an image inspect answer the router reads.
type ImageSummary struct {
	ID       string   `json:"Id"`
	RepoTags []string `json:"RepoTags"`
}

// ImageInspect reads one image by reference or ID.
func (client *Client) ImageInspect(ctx context.Context, reference string) (ImageSummary, error) {
	var summary ImageSummary
	_, err := client.doJSON(ctx, http.MethodGet, "/images/"+reference+"/json", nil, nil, &summary)
	return summary, err
}

// SplitReference splits an image reference into the repository and the tag
// or digest the pull API takes separately.
func SplitReference(reference string) (string, string) {
	if at := strings.LastIndex(reference, "@"); at >= 0 {
		return reference[:at], reference[at+1:]
	}
	slash := strings.LastIndex(reference, "/")
	if colon := strings.LastIndex(reference, ":"); colon > slash {
		return reference[:colon], reference[colon+1:]
	}
	return reference, "latest"
}

// ImagePull pulls one reference. registryAuth is the base64 X-Registry-Auth
// value, empty for anonymous pulls. onProgress receives every progress line
// (nil ignores them).
func (client *Client) ImagePull(ctx context.Context, reference, registryAuth string, onProgress func(ProgressEvent)) error {
	repository, tag := SplitReference(reference)
	query := url.Values{"fromImage": {repository}, "tag": {tag}}
	headers := http.Header{}
	if registryAuth != "" {
		headers.Set("X-Registry-Auth", registryAuth)
	}
	response, err := client.do(ctx, request{method: http.MethodPost, path: "/images/create", query: query, headers: headers})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	return readProgress(response.Body, onProgress)
}

// ImageImport creates an image from a root filesystem tarball. changes are
// Dockerfile instructions such as CMD applied to the image config.
func (client *Client) ImageImport(ctx context.Context, tarball io.Reader, repository, tag string, changes []string) error {
	query := url.Values{"fromSrc": {"-"}, "repo": {repository}, "tag": {tag}}
	for _, change := range changes {
		query.Add("changes", change)
	}
	response, err := client.do(ctx, request{method: http.MethodPost, path: "/images/create", query: query, body: tarball, contentType: "application/x-tar"})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	return readProgress(response.Body, nil)
}

// BuildOptions configures one image build.
type BuildOptions struct {
	Tag        string
	Dockerfile string
	Labels     map[string]string
	// MemoryBytes and NanoCPUs bound the build containers.
	MemoryBytes int64
	NanoCPUs    int64
	// RegistryConfig is the base64 X-Registry-Config value for base image
	// pulls, empty for anonymous pulls.
	RegistryConfig string
	// Progress receives every progress line (nil ignores them).
	Progress func(ProgressEvent)
}

// buildCPUPeriod is the CFS period the CPU quota of a build refers to.
const buildCPUPeriod = 100000

// ImageBuild builds an image from a context tarball with the classic
// builder.
func (client *Client) ImageBuild(ctx context.Context, contextTar io.Reader, options BuildOptions) error {
	query := url.Values{"t": {options.Tag}, "rm": {"1"}, "forcerm": {"1"}, "version": {"1"}}
	if options.Dockerfile != "" {
		query.Set("dockerfile", options.Dockerfile)
	}
	if len(options.Labels) > 0 {
		encoded, err := json.Marshal(options.Labels)
		if err != nil {
			return err
		}
		query.Set("labels", string(encoded))
	}
	if options.MemoryBytes > 0 {
		query.Set("memory", strconv.FormatInt(options.MemoryBytes, 10))
		query.Set("memswap", strconv.FormatInt(options.MemoryBytes, 10))
	}
	if options.NanoCPUs > 0 {
		query.Set("cpuperiod", strconv.Itoa(buildCPUPeriod))
		query.Set("cpuquota", strconv.FormatInt(options.NanoCPUs*buildCPUPeriod/1e9, 10))
	}
	headers := http.Header{}
	if options.RegistryConfig != "" {
		headers.Set("X-Registry-Config", options.RegistryConfig)
	}
	response, err := client.do(ctx, request{method: http.MethodPost, path: "/build", query: query, body: contextTar, contentType: "application/x-tar", headers: headers})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	return readProgress(response.Body, options.Progress)
}

// HealthConfig is a container health check. Durations are nanoseconds.
type HealthConfig struct {
	Test        []string `json:"Test,omitempty"`
	Interval    int64    `json:"Interval,omitempty"`
	Timeout     int64    `json:"Timeout,omitempty"`
	StartPeriod int64    `json:"StartPeriod,omitempty"`
	Retries     int      `json:"Retries,omitempty"`
}

// RestartPolicy is the daemon restart rule.
type RestartPolicy struct {
	Name              string `json:"Name"`
	MaximumRetryCount int    `json:"MaximumRetryCount,omitempty"`
}

// LogConfig selects the log driver.
type LogConfig struct {
	Type   string            `json:"Type"`
	Config map[string]string `json:"Config,omitempty"`
}

// Mount is one volume mount.
type Mount struct {
	Type   string `json:"Type"`
	Source string `json:"Source"`
	Target string `json:"Target"`
}

// DeviceRequest requests devices such as GPUs.
type DeviceRequest struct {
	Driver       string     `json:"Driver,omitempty"`
	Count        int        `json:"Count"`
	Capabilities [][]string `json:"Capabilities"`
}

// HostConfig is the subset of host settings the router sets.
type HostConfig struct {
	Memory         int64           `json:"Memory,omitempty"`
	MemorySwap     int64           `json:"MemorySwap,omitempty"`
	NanoCPUs       int64           `json:"NanoCpus,omitempty"`
	PidsLimit      int64           `json:"PidsLimit,omitempty"`
	ShmSize        int64           `json:"ShmSize,omitempty"`
	CapDrop        []string        `json:"CapDrop,omitempty"`
	SecurityOpt    []string        `json:"SecurityOpt,omitempty"`
	Init           bool            `json:"Init"`
	Privileged     bool            `json:"Privileged"`
	ReadonlyRootfs bool            `json:"ReadonlyRootfs"`
	NetworkMode    string          `json:"NetworkMode,omitempty"`
	RestartPolicy  RestartPolicy   `json:"RestartPolicy"`
	LogConfig      LogConfig       `json:"LogConfig"`
	Mounts         []Mount         `json:"Mounts,omitempty"`
	DeviceRequests []DeviceRequest `json:"DeviceRequests,omitempty"`
}

// EndpointSettings attaches a container to one network.
type EndpointSettings struct {
	Aliases []string `json:"Aliases,omitempty"`
}

// NetworkingConfig lists the networks a container joins at creation.
type NetworkingConfig struct {
	EndpointsConfig map[string]EndpointSettings `json:"EndpointsConfig"`
}

// ContainerConfig is the POST /containers/create body.
type ContainerConfig struct {
	Hostname         string              `json:"Hostname,omitempty"`
	User             string              `json:"User,omitempty"`
	Env              []string            `json:"Env,omitempty"`
	Cmd              []string            `json:"Cmd,omitempty"`
	Entrypoint       []string            `json:"Entrypoint,omitempty"`
	Image            string              `json:"Image"`
	WorkingDir       string              `json:"WorkingDir,omitempty"`
	Labels           map[string]string   `json:"Labels,omitempty"`
	ExposedPorts     map[string]struct{} `json:"ExposedPorts,omitempty"`
	Healthcheck      *HealthConfig       `json:"Healthcheck,omitempty"`
	HostConfig       HostConfig          `json:"HostConfig"`
	NetworkingConfig *NetworkingConfig   `json:"NetworkingConfig,omitempty"`
}

// ContainerCreate creates one container and returns its ID.
func (client *Client) ContainerCreate(ctx context.Context, name string, config ContainerConfig) (string, error) {
	var created struct {
		ID string `json:"Id"`
	}
	_, err := client.doJSON(ctx, http.MethodPost, "/containers/create", url.Values{"name": {name}}, config, &created)
	return created.ID, err
}

// ContainerStart starts a container. Already running is not an error.
func (client *Client) ContainerStart(ctx context.Context, id string) error {
	_, err := client.doJSON(ctx, http.MethodPost, "/containers/"+id+"/start", nil, nil, nil)
	return err
}

// ContainerStop stops a container. Already stopped is not an error.
func (client *Client) ContainerStop(ctx context.Context, id string, timeoutSeconds int) error {
	_, err := client.doJSON(ctx, http.MethodPost, "/containers/"+id+"/stop", url.Values{"t": {strconv.Itoa(timeoutSeconds)}}, nil, nil)
	return err
}

// ContainerRemove removes a container, running or not.
func (client *Client) ContainerRemove(ctx context.Context, id string) error {
	_, err := client.doJSON(ctx, http.MethodDelete, "/containers/"+id, url.Values{"force": {"1"}, "v": {"0"}}, nil, nil)
	return err
}

// ContainerHealth is the health part of a container state.
type ContainerHealth struct {
	Status string `json:"Status"`
}

// ContainerState is the state part of a container inspect answer.
type ContainerState struct {
	Status    string           `json:"Status"`
	Running   bool             `json:"Running"`
	ExitCode  int              `json:"ExitCode"`
	StartedAt string           `json:"StartedAt"`
	Health    *ContainerHealth `json:"Health"`
}

// ContainerNetwork is one network attachment.
type ContainerNetwork struct {
	NetworkID string `json:"NetworkID"`
	IPAddress string `json:"IPAddress"`
}

// ContainerDetails is the subset of a container inspect answer the router
// reads.
type ContainerDetails struct {
	ID     string         `json:"Id"`
	Name   string         `json:"Name"`
	State  ContainerState `json:"State"`
	Config struct {
		Image  string            `json:"Image"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	NetworkSettings struct {
		Networks map[string]ContainerNetwork `json:"Networks"`
	} `json:"NetworkSettings"`
	Mounts []ContainerMount `json:"Mounts"`
}

// ContainerMount is one mount of an inspected container.
type ContainerMount struct {
	Type        string `json:"Type"`
	Name        string `json:"Name"`
	Destination string `json:"Destination"`
}

// ContainerInspect reads one container by ID or name.
func (client *Client) ContainerInspect(ctx context.Context, id string) (ContainerDetails, error) {
	var details ContainerDetails
	_, err := client.doJSON(ctx, http.MethodGet, "/containers/"+id+"/json", nil, nil, &details)
	return details, err
}

// ContainerSummary is one entry of a container list.
type ContainerSummary struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	State  string            `json:"State"`
	Labels map[string]string `json:"Labels"`
}

// labelFilter renders label=value filters for list endpoints.
func labelFilter(labels map[string]string) (string, error) {
	values := make([]string, 0, len(labels))
	for name, value := range labels {
		values = append(values, name+"="+value)
	}
	encoded, err := json.Marshal(map[string][]string{"label": values})
	return string(encoded), err
}

// ContainerList lists every container, stopped ones included, that carries
// all the given labels.
func (client *Client) ContainerList(ctx context.Context, labels map[string]string) ([]ContainerSummary, error) {
	filters, err := labelFilter(labels)
	if err != nil {
		return nil, err
	}
	summaries := []ContainerSummary{}
	_, err = client.doJSON(ctx, http.MethodGet, "/containers/json", url.Values{"all": {"1"}, "filters": {filters}}, nil, &summaries)
	return summaries, err
}

// ContainerLogs reads demultiplexed logs. tail 0 reads all lines; since is
// a Unix timestamp, 0 for no bound.
func (client *Client) ContainerLogs(ctx context.Context, id string, tail int, since int64, timestamps bool, limit int) (string, string, error) {
	query := url.Values{"stdout": {"1"}, "stderr": {"1"}}
	if tail > 0 {
		query.Set("tail", strconv.Itoa(tail))
	}
	if since > 0 {
		query.Set("since", strconv.FormatInt(since, 10))
	}
	if timestamps {
		query.Set("timestamps", "1")
	}
	response, err := client.do(ctx, request{method: http.MethodGet, path: "/containers/" + id + "/logs", query: query})
	if err != nil {
		return "", "", err
	}
	defer response.Body.Close()
	stdout, stderr, err := Demultiplex(response.Body, limit)
	return string(stdout), string(stderr), err
}

// ContainerPutArchive extracts a tar archive into path inside a container.
func (client *Client) ContainerPutArchive(ctx context.Context, id, path string, archive io.Reader) error {
	response, err := client.do(ctx, request{method: http.MethodPut, path: "/containers/" + id + "/archive", query: url.Values{"path": {path}}, body: archive, contentType: "application/x-tar"})
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, response.Body)
	return response.Body.Close()
}

// ExecConfig is the POST /containers/{id}/exec body.
type ExecConfig struct {
	Cmd          []string `json:"Cmd"`
	Env          []string `json:"Env,omitempty"`
	User         string   `json:"User,omitempty"`
	WorkingDir   string   `json:"WorkingDir,omitempty"`
	AttachStdin  bool     `json:"AttachStdin"`
	AttachStdout bool     `json:"AttachStdout"`
	AttachStderr bool     `json:"AttachStderr"`
	Tty          bool     `json:"Tty"`
}

// ExecCreate creates one exec instance and returns its ID.
func (client *Client) ExecCreate(ctx context.Context, containerID string, config ExecConfig) (string, error) {
	var created struct {
		ID string `json:"Id"`
	}
	_, err := client.doJSON(ctx, http.MethodPost, "/containers/"+containerID+"/exec", nil, config, &created)
	return created.ID, err
}

// ExecDetails is the GET /exec/{id}/json answer.
type ExecDetails struct {
	Running  bool `json:"Running"`
	ExitCode int  `json:"ExitCode"`
}

// ExecInspect reads one exec instance.
func (client *Client) ExecInspect(ctx context.Context, execID string) (ExecDetails, error) {
	var details ExecDetails
	_, err := client.doJSON(ctx, http.MethodGet, "/exec/"+execID+"/json", nil, nil, &details)
	return details, err
}

// NetworkDetails is the subset of a network inspect answer the router reads.
type NetworkDetails struct {
	ID       string            `json:"Id"`
	Name     string            `json:"Name"`
	Internal bool              `json:"Internal"`
	Labels   map[string]string `json:"Labels"`
}

// NetworkCreate creates a bridge network and returns its ID.
func (client *Client) NetworkCreate(ctx context.Context, name string, internal bool, labels map[string]string) (string, error) {
	body := map[string]any{"Name": name, "Driver": "bridge", "Internal": internal, "Labels": labels, "CheckDuplicate": true}
	var created struct {
		ID string `json:"Id"`
	}
	_, err := client.doJSON(ctx, http.MethodPost, "/networks/create", nil, body, &created)
	return created.ID, err
}

// NetworkInspect reads one network by ID or name.
func (client *Client) NetworkInspect(ctx context.Context, id string) (NetworkDetails, error) {
	var details NetworkDetails
	_, err := client.doJSON(ctx, http.MethodGet, "/networks/"+id, nil, nil, &details)
	return details, err
}

// NetworkConnect attaches a container to a network. Callers check the
// container's attachments first: the daemon refuses a second attachment.
func (client *Client) NetworkConnect(ctx context.Context, networkID, containerID string, aliases []string) error {
	body := map[string]any{"Container": containerID, "EndpointConfig": EndpointSettings{Aliases: aliases}}
	_, err := client.doJSON(ctx, http.MethodPost, "/networks/"+networkID+"/connect", nil, body, nil)
	return err
}

// NetworkDisconnect detaches a container from a network.
func (client *Client) NetworkDisconnect(ctx context.Context, networkID, containerID string) error {
	body := map[string]any{"Container": containerID, "Force": true}
	_, err := client.doJSON(ctx, http.MethodPost, "/networks/"+networkID+"/disconnect", nil, body, nil)
	return err
}

// NetworkList lists networks carrying all the given labels.
func (client *Client) NetworkList(ctx context.Context, labels map[string]string) ([]NetworkDetails, error) {
	filters, err := labelFilter(labels)
	if err != nil {
		return nil, err
	}
	networks := []NetworkDetails{}
	_, err = client.doJSON(ctx, http.MethodGet, "/networks", url.Values{"filters": {filters}}, nil, &networks)
	return networks, err
}

// NetworkRemove removes one network.
func (client *Client) NetworkRemove(ctx context.Context, id string) error {
	_, err := client.doJSON(ctx, http.MethodDelete, "/networks/"+id, nil, nil, nil)
	return err
}

// VolumeDetails is one named volume.
type VolumeDetails struct {
	Name   string            `json:"Name"`
	Labels map[string]string `json:"Labels"`
}

// VolumeCreate creates a named volume. An existing name is returned as is.
func (client *Client) VolumeCreate(ctx context.Context, name string, labels map[string]string) (VolumeDetails, error) {
	var details VolumeDetails
	_, err := client.doJSON(ctx, http.MethodPost, "/volumes/create", nil, map[string]any{"Name": name, "Labels": labels}, &details)
	return details, err
}

// VolumeList lists volumes carrying all the given labels.
func (client *Client) VolumeList(ctx context.Context, labels map[string]string) ([]VolumeDetails, error) {
	filters, err := labelFilter(labels)
	if err != nil {
		return nil, err
	}
	var listed struct {
		Volumes []VolumeDetails `json:"Volumes"`
	}
	_, err = client.doJSON(ctx, http.MethodGet, "/volumes", url.Values{"filters": {filters}}, nil, &listed)
	if listed.Volumes == nil {
		listed.Volumes = []VolumeDetails{}
	}
	return listed.Volumes, err
}

// VolumeRemove removes one named volume.
func (client *Client) VolumeRemove(ctx context.Context, name string) error {
	_, err := client.doJSON(ctx, http.MethodDelete, "/volumes/"+name, url.Values{"force": {"1"}}, nil, nil)
	return err
}

// ImageListEntry is one entry of an image list.
type ImageListEntry struct {
	ID       string            `json:"Id"`
	RepoTags []string          `json:"RepoTags"`
	Labels   map[string]string `json:"Labels"`
}

// ImageList lists images carrying all the given labels.
func (client *Client) ImageList(ctx context.Context, labels map[string]string) ([]ImageListEntry, error) {
	filters, err := labelFilter(labels)
	if err != nil {
		return nil, err
	}
	images := []ImageListEntry{}
	_, err = client.doJSON(ctx, http.MethodGet, "/images/json", url.Values{"filters": {filters}}, nil, &images)
	return images, err
}

// ImageRemove removes one image reference. A missing image is not an error.
func (client *Client) ImageRemove(ctx context.Context, reference string) error {
	_, err := client.doJSON(ctx, http.MethodDelete, "/images/"+reference, url.Values{"force": {"1"}}, nil, nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("remove image %s: %w", reference, err)
	}
	return nil
}

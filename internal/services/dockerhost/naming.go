package dockerhost

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Labels on every object the router creates. Labels own identity; names
// only give uniqueness and readability.
const (
	labelNamespace  = "io.llm-router.namespace"
	labelPlugin     = "io.llm-router.plugin"
	labelKey        = "io.llm-router.key"
	labelService    = "io.llm-router.service"
	labelRole       = "io.llm-router.role"
	labelSpec       = "io.llm-router.spec"
	labelPorts      = "io.llm-router.ports"
	labelProxyToken = "io.llm-router.proxy-token"
	// labelDependsOn lists service:condition pairs of a compose service.
	labelDependsOn = "io.llm-router.depends-on"
)

// Object roles.
const (
	roleContainer = "container"
	roleService   = "service"
	roleGateway   = "gateway"
	roleNetwork   = "network"
	roleEgress    = "egress"
	roleVolume    = "volume"
	roleImage     = "image"
)

// gatewayAlias is the gateway host name on every plugin network.
const gatewayAlias = "llmr-gateway"

// gatewayProxyPort is the proxy port inside the gateway container.
const gatewayProxyPort = "3128"

// gatewayBinaryPath is the gateway program inside its image.
const gatewayBinaryPath = "/gateway"

// shortHash returns the first characters of the SHA-256 of the parts.
func shortHash(length int, parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])[:length]
}

// nameSafe keeps characters Docker accepts in object names.
func nameSafe(value string) string {
	var builder strings.Builder
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z', character >= 'A' && character <= 'Z', character >= '0' && character <= '9', character == '_', character == '.', character == '-':
			builder.WriteRune(character)
		default:
			builder.WriteRune('-')
		}
	}
	return builder.String()
}

// objectName builds <namespace>.<author>.<plugin>[.<parts>]-<hash8>. The
// hash covers namespace, plugin identity and parts, so distinct owners
// never share a name even when author or plugin names contain dots.
func objectName(namespace, pluginID string, parts ...string) string {
	readable := append([]string{namespace, strings.ReplaceAll(pluginID, "/", ".")}, parts...)
	hashParts := append([]string{namespace, pluginID}, parts...)
	return nameSafe(strings.Join(readable, ".")) + "-" + shortHash(8, hashParts...)
}

// target identifies one container: a standalone key, or a compose project
// key plus service.
type target struct {
	key     string
	service string
}

func (target target) String() string {
	if target.service == "" {
		return target.key
	}
	return target.key + "/" + target.service
}

// parseTarget reads "key" or "project/service".
func parseTarget(text string) (target, error) {
	key, service, compose := strings.Cut(text, "/")
	if !ValidKey(key) || (compose && !ValidKey(service)) {
		return target{}, specError("target %q must be a key or project/service matching %s", text, keyPattern.String())
	}
	return target{key: key, service: service}, nil
}

func (session *session) containerName(pluginID string, target target) string {
	if target.service == "" {
		return objectName(session.namespace, pluginID, target.key)
	}
	return objectName(session.namespace, pluginID, target.key, target.service)
}

// networkName is the internal network of standalone containers (project
// "") or of one compose project.
func (session *session) networkName(pluginID, project string) string {
	if project == "" {
		return objectName(session.namespace, pluginID, "network")
	}
	return objectName(session.namespace, pluginID, project, "network")
}

// volumeName scopes standalone volumes to the plugin and compose volumes to
// the project.
func (session *session) volumeName(pluginID, project, volume string) string {
	if project == "" {
		return objectName(session.namespace, pluginID, "volume", volume)
	}
	return objectName(session.namespace, pluginID, project, "volume", volume)
}

func (session *session) gatewayName(pluginID string) string {
	return objectName(session.namespace, pluginID, "gateway")
}

// egressNetworkName is the bridge every gateway uses to reach out.
func (session *session) egressNetworkName() string {
	return session.namespace + ".egress"
}

// buildTag is the image tag of one declared build. The hash covers the
// source, so a changed URL or Dockerfile builds a new image.
func (session *session) buildTag(pluginID, name string, source BuildSource) string {
	component := repositoryComponent(session.namespace + "." + strings.ReplaceAll(pluginID, "/", ".") + "." + name)
	return "llmr-build/" + component + ":" + shortHash(12, session.namespace, pluginID, name, source.URL, source.Dockerfile)
}

// repositoryComponent maps text to one image repository path component:
// lowercase alphanumerics joined by single separators.
func repositoryComponent(text string) string {
	var builder strings.Builder
	separator := false
	for _, character := range strings.ToLower(text) {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
			if separator && builder.Len() > 0 {
				builder.WriteByte('.')
			}
			separator = false
			builder.WriteRune(character)
			continue
		}
		separator = true
	}
	if builder.Len() == 0 {
		return "image"
	}
	return builder.String()
}

// ownerLabels are the labels every object of one plugin carries.
func (session *session) ownerLabels(pluginID, role string) map[string]string {
	return map[string]string{labelNamespace: session.namespace, labelPlugin: pluginID, labelRole: role}
}

// namespaceLabels select every object of this router.
func (session *session) namespaceLabels() map[string]string {
	return map[string]string{labelNamespace: session.namespace}
}

package dockerhost

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"

	"go.yaml.in/yaml/v3"
)

// maximumComposeBytes caps one compose YAML document.
const maximumComposeBytes = 256 << 10

// ParseCompose reads a compose YAML subset into a project. ${VAR} and
// ${VAR:-default} resolve from variables only. Keys outside the subset
// fail with their path; x-* keys are ignored.
func ParseCompose(text string, variables map[string]string, policy Policy) (ProjectSpec, error) {
	if len(text) > maximumComposeBytes {
		return ProjectSpec{}, specError("compose: yaml exceeds %d bytes", maximumComposeBytes)
	}
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(text), &document); err != nil {
		return ProjectSpec{}, specError("compose: %v", err)
	}
	if len(document.Content) != 1 {
		return ProjectSpec{}, specError("compose: yaml must hold one mapping")
	}
	parser := composeParser{variables: variables, policy: policy}
	root := document.Content[0]
	if err := parser.interpolate(root, ""); err != nil {
		return ProjectSpec{}, err
	}
	project := ProjectSpec{Services: map[string]ContainerSpec{}}
	declaredVolumes := map[string]bool{}
	err := parser.mapping(root, "", func(key string, value *yaml.Node, field string) error {
		switch key {
		case "version", "name":
			return nil
		case "services":
			return parser.mapping(value, field, func(name string, serviceNode *yaml.Node, serviceField string) error {
				service, err := parser.service(serviceNode, serviceField)
				if err != nil {
					return err
				}
				project.Services[name] = service
				return nil
			})
		case "volumes":
			return parser.mapping(value, field, func(name string, volumeNode *yaml.Node, volumeField string) error {
				if !ValidKey(name) {
					return specError("%s: volume name must match %s", volumeField, keyPattern.String())
				}
				if volumeNode.Kind == yaml.MappingNode && len(volumeNode.Content) > 0 {
					return parser.mapping(volumeNode, volumeField, func(option string, _ *yaml.Node, optionField string) error {
						return specError("%s: volume options are not supported", optionField)
					})
				}
				declaredVolumes[name] = true
				return nil
			})
		default:
			return specError("%s: not supported", field)
		}
	})
	if err != nil {
		return ProjectSpec{}, err
	}
	for name, service := range project.Services {
		for volume := range service.Volumes {
			if !declaredVolumes[volume] {
				return ProjectSpec{}, specError("services.%s.volumes: volume %q is not declared in the top-level volumes", name, volume)
			}
		}
	}
	return project, nil
}

type composeParser struct {
	variables map[string]string
	policy    Policy
}

func join(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

// mapping walks a mapping node, skipping x-* keys.
func (parser composeParser) mapping(node *yaml.Node, field string, visit func(key string, value *yaml.Node, field string) error) error {
	if node.Kind == yaml.ScalarNode && node.Tag == "!!null" {
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return specError("%s: must be a mapping", fieldName(field))
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		key := node.Content[index].Value
		if strings.HasPrefix(key, "x-") {
			continue
		}
		if err := visit(key, node.Content[index+1], join(field, key)); err != nil {
			return err
		}
	}
	return nil
}

func fieldName(field string) string {
	if field == "" {
		return "compose"
	}
	return field
}

// interpolate resolves variables in every scalar value below node.
func (parser composeParser) interpolate(node *yaml.Node, field string) error {
	switch node.Kind {
	case yaml.ScalarNode:
		if !strings.Contains(node.Value, "$") {
			return nil
		}
		resolved, err := interpolateText(node.Value, parser.variables)
		if err != nil {
			return specError("%s: %v", fieldName(field), err)
		}
		node.Value = resolved
		if node.Tag != "!!str" {
			node.Tag = ""
		}
	case yaml.MappingNode:
		for index := 0; index+1 < len(node.Content); index += 2 {
			if err := parser.interpolate(node.Content[index+1], join(field, node.Content[index].Value)); err != nil {
				return err
			}
		}
	case yaml.SequenceNode, yaml.DocumentNode:
		for index, child := range node.Content {
			if err := parser.interpolate(child, fmt.Sprintf("%s[%d]", fieldName(field), index)); err != nil {
				return err
			}
		}
	case yaml.AliasNode:
		return specError("%s: yaml aliases are not supported", fieldName(field))
	}
	return nil
}

// interpolateText resolves $VAR, ${VAR}, ${VAR:-default}, ${VAR-default},
// ${VAR:?message} and ${VAR?message}. $$ is a literal dollar sign.
func interpolateText(text string, variables map[string]string) (string, error) {
	var builder strings.Builder
	for index := 0; index < len(text); index++ {
		character := text[index]
		if character != '$' {
			builder.WriteByte(character)
			continue
		}
		if index+1 >= len(text) {
			return "", fmt.Errorf("dangling $ at the end of %q", text)
		}
		next := text[index+1]
		switch {
		case next == '$':
			builder.WriteByte('$')
			index++
		case next == '{':
			end := strings.IndexByte(text[index:], '}')
			if end < 0 {
				return "", fmt.Errorf("unclosed ${ in %q", text)
			}
			expression := text[index+2 : index+end]
			value, err := resolveExpression(expression, variables)
			if err != nil {
				return "", err
			}
			builder.WriteString(value)
			index += end
		case next == '_' || unicode.IsLetter(rune(next)):
			end := index + 1
			for end < len(text) && (text[end] == '_' || unicode.IsLetter(rune(text[end])) || unicode.IsDigit(rune(text[end]))) {
				end++
			}
			value, err := resolveExpression(text[index+1:end], variables)
			if err != nil {
				return "", err
			}
			builder.WriteString(value)
			index = end - 1
		default:
			return "", fmt.Errorf("invalid $ in %q: use $$ for a literal dollar sign", text)
		}
	}
	return builder.String(), nil
}

func resolveExpression(expression string, variables map[string]string) (string, error) {
	for _, operator := range []string{":-", ":?", "-", "?"} {
		name, argument, found := strings.Cut(expression, operator)
		if !found || !environmentNamePattern.MatchString(name) {
			continue
		}
		value, set := variables[name]
		empty := !set || (strings.HasPrefix(operator, ":") && value == "")
		switch operator {
		case ":-", "-":
			if empty {
				return argument, nil
			}
			return value, nil
		default:
			if empty {
				return "", fmt.Errorf("variable %s: %s", name, argument)
			}
			return value, nil
		}
	}
	if !environmentNamePattern.MatchString(expression) {
		return "", fmt.Errorf("invalid variable expression ${%s}", expression)
	}
	value, set := variables[expression]
	if !set {
		return "", fmt.Errorf("variable %s is not set and has no default", expression)
	}
	return value, nil
}

func (parser composeParser) service(node *yaml.Node, field string) (ContainerSpec, error) {
	var spec ContainerSpec
	err := parser.mapping(node, field, func(key string, value *yaml.Node, keyField string) error {
		var err error
		switch key {
		case "image":
			spec.Image, err = scalar(value, keyField)
		case "build":
			spec.Build, err = parser.build(value, keyField)
		case "command":
			spec.Command, err = commandValue(value, keyField)
		case "entrypoint":
			spec.Entrypoint, err = commandValue(value, keyField)
		case "environment":
			spec.Environment, err = environmentValue(value, keyField)
		case "working_dir":
			spec.WorkingDirectory, err = scalar(value, keyField)
		case "user":
			spec.User, err = scalar(value, keyField)
		case "expose":
			spec.Ports, err = exposeValue(value, keyField)
		case "healthcheck":
			spec.Health, err = parser.healthcheck(value, keyField)
		case "depends_on":
			spec.DependsOn, err = parser.dependsOn(value, keyField)
		case "volumes":
			spec.Volumes, err = volumesValue(value, keyField)
		case "deploy":
			err = parser.deploy(value, keyField, &spec)
		case "shm_size":
			spec.ShmSizeMB, err = megabytesValue(value, keyField)
		case "restart":
			spec.Restart, err = restartValue(value, keyField)
		case "read_only":
			spec.ReadOnly, err = booleanValue(value, keyField)
		case "ports":
			err = specError("%s: publishing is not supported, use expose", keyField)
		default:
			err = specError("%s: not supported", keyField)
		}
		return err
	})
	return spec, err
}

func scalar(node *yaml.Node, field string) (string, error) {
	if node.Kind != yaml.ScalarNode {
		return "", specError("%s: must be a scalar", field)
	}
	return node.Value, nil
}

func booleanValue(node *yaml.Node, field string) (bool, error) {
	text, err := scalar(node, field)
	if err != nil {
		return false, err
	}
	value, err := strconv.ParseBool(text)
	if err != nil {
		return false, specError("%s: must be true or false", field)
	}
	return value, nil
}

func stringList(node *yaml.Node, field string) ([]string, error) {
	if node.Kind != yaml.SequenceNode {
		return nil, specError("%s: must be a list", field)
	}
	values := make([]string, 0, len(node.Content))
	for index, child := range node.Content {
		value, err := scalar(child, fmt.Sprintf("%s[%d]", field, index))
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

// commandValue reads a list, or a string split like a POSIX shell word list.
func commandValue(node *yaml.Node, field string) ([]string, error) {
	if node.Kind == yaml.SequenceNode {
		return stringList(node, field)
	}
	text, err := scalar(node, field)
	if err != nil {
		return nil, err
	}
	words, err := splitWords(text)
	if err != nil {
		return nil, specError("%s: %v", field, err)
	}
	return words, nil
}

// splitWords splits text on unquoted whitespace. Single quotes keep
// everything; double quotes and backslashes escape.
func splitWords(text string) ([]string, error) {
	var words []string
	var current strings.Builder
	inWord := false
	var quote rune
	escaped := false
	for _, character := range text {
		switch {
		case escaped:
			current.WriteRune(character)
			escaped = false
		case quote == '\'':
			if character == '\'' {
				quote = 0
			} else {
				current.WriteRune(character)
			}
		case character == '\\':
			escaped = true
			inWord = true
		case quote == '"':
			if character == '"' {
				quote = 0
			} else {
				current.WriteRune(character)
			}
		case character == '\'' || character == '"':
			quote = character
			inWord = true
		case unicode.IsSpace(character):
			if inWord {
				words = append(words, current.String())
				current.Reset()
				inWord = false
			}
		default:
			current.WriteRune(character)
			inWord = true
		}
	}
	if quote != 0 || escaped {
		return nil, fmt.Errorf("unterminated quote or escape")
	}
	if inWord {
		words = append(words, current.String())
	}
	return words, nil
}

func environmentValue(node *yaml.Node, field string) (map[string]string, error) {
	environment := map[string]string{}
	switch node.Kind {
	case yaml.MappingNode:
		for index := 0; index+1 < len(node.Content); index += 2 {
			value, err := scalar(node.Content[index+1], join(field, node.Content[index].Value))
			if err != nil {
				return nil, err
			}
			environment[node.Content[index].Value] = value
		}
	case yaml.SequenceNode:
		entries, err := stringList(node, field)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			name, value, found := strings.Cut(entry, "=")
			if !found {
				return nil, specError("%s: entry %q must be NAME=value", field, entry)
			}
			environment[name] = value
		}
	default:
		return nil, specError("%s: must be a mapping or a list", field)
	}
	return environment, nil
}

func exposeValue(node *yaml.Node, field string) ([]int, error) {
	entries, err := stringList(node, field)
	if err != nil {
		return nil, err
	}
	ports := make([]int, 0, len(entries))
	for _, entry := range entries {
		number, protocol, _ := strings.Cut(entry, "/")
		if protocol != "" && protocol != "tcp" {
			return nil, specError("%s: only tcp ports are supported, got %q", field, entry)
		}
		port, err := strconv.Atoi(number)
		if err != nil {
			return nil, specError("%s: port %q must be a number", field, entry)
		}
		ports = append(ports, port)
	}
	return ports, nil
}

func volumesValue(node *yaml.Node, field string) (map[string]string, error) {
	if node.Kind != yaml.SequenceNode {
		return nil, specError("%s: must be a list", field)
	}
	volumes := map[string]string{}
	for index, child := range node.Content {
		entryField := fmt.Sprintf("%s[%d]", field, index)
		var source, target string
		switch child.Kind {
		case yaml.ScalarNode:
			parts := strings.Split(child.Value, ":")
			if len(parts) < 2 || len(parts) > 3 {
				return nil, specError("%s: must be NAME:/path", entryField)
			}
			if len(parts) == 3 && parts[2] != "rw" {
				return nil, specError("%s: mount options are not supported", entryField)
			}
			source, target = parts[0], parts[1]
		case yaml.MappingNode:
			kind := ""
			for position := 0; position+1 < len(child.Content); position += 2 {
				key, value := child.Content[position].Value, child.Content[position+1].Value
				switch key {
				case "type":
					kind = value
				case "source":
					source = value
				case "target":
					target = value
				default:
					return nil, specError("%s.%s: not supported", entryField, key)
				}
			}
			if kind != "volume" {
				return nil, specError("%s: only type volume is supported (bind mounts are not)", entryField)
			}
		default:
			return nil, specError("%s: must be a string or a mapping", entryField)
		}
		if !ValidKey(source) {
			return nil, specError("%s: %q is not a named volume (bind mounts are not supported)", entryField, source)
		}
		volumes[source] = target
	}
	return volumes, nil
}

func (parser composeParser) build(node *yaml.Node, field string) (string, error) {
	context, dockerfile := "", defaultDockerfile
	switch node.Kind {
	case yaml.ScalarNode:
		context = node.Value
	case yaml.MappingNode:
		err := parser.mapping(node, field, func(key string, value *yaml.Node, keyField string) error {
			var err error
			switch key {
			case "context":
				context, err = scalar(value, keyField)
			case "dockerfile":
				dockerfile, err = scalar(value, keyField)
			default:
				err = specError("%s: not supported", keyField)
			}
			return err
		})
		if err != nil {
			return "", err
		}
	default:
		return "", specError("%s: must be a URL or a mapping", field)
	}
	for name, source := range parser.policy.Directives.Builds {
		if source.URL == context && source.Dockerfile == dockerfile {
			return name, nil
		}
	}
	return "", specError("%s: context %q with Dockerfile %q is not declared with @docker.build", field, context, dockerfile)
}

func (parser composeParser) healthcheck(node *yaml.Node, field string) (*HealthSpec, error) {
	health := &HealthSpec{}
	disabled := false
	err := parser.mapping(node, field, func(key string, value *yaml.Node, keyField string) error {
		var err error
		switch key {
		case "test":
			health.Command, err = healthTest(value, keyField)
		case "interval":
			health.IntervalSeconds, err = secondsValue(value, keyField)
		case "timeout":
			health.TimeoutSeconds, err = secondsValue(value, keyField)
		case "start_period":
			health.StartPeriodSeconds, err = secondsValue(value, keyField)
		case "retries":
			var text string
			text, err = scalar(value, keyField)
			if err == nil {
				health.Retries, err = strconv.Atoi(text)
				if err != nil {
					err = specError("%s: must be a whole number", keyField)
				}
			}
		case "disable":
			disabled, err = booleanValue(value, keyField)
		default:
			err = specError("%s: not supported", keyField)
		}
		return err
	})
	if err != nil || disabled {
		return nil, err
	}
	return health, nil
}

// healthTest maps CMD and CMD-SHELL forms to an argument list.
func healthTest(node *yaml.Node, field string) ([]string, error) {
	if node.Kind == yaml.ScalarNode {
		return []string{"/bin/sh", "-c", node.Value}, nil
	}
	values, err := stringList(node, field)
	if err != nil {
		return nil, err
	}
	if len(values) < 2 {
		return nil, specError("%s: must be [CMD, ...] or [CMD-SHELL, command]", field)
	}
	switch values[0] {
	case "CMD":
		return values[1:], nil
	case "CMD-SHELL":
		return []string{"/bin/sh", "-c", strings.Join(values[1:], " ")}, nil
	default:
		return nil, specError("%s: must start with CMD or CMD-SHELL", field)
	}
}

func secondsValue(node *yaml.Node, field string) (float64, error) {
	text, err := scalar(node, field)
	if err != nil {
		return 0, err
	}
	duration, err := time.ParseDuration(text)
	if err != nil {
		return 0, specError("%s: %q is not a duration such as 10s", field, text)
	}
	return duration.Seconds(), nil
}

func (parser composeParser) dependsOn(node *yaml.Node, field string) (map[string]string, error) {
	dependencies := map[string]string{}
	if node.Kind == yaml.SequenceNode {
		names, err := stringList(node, field)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			dependencies[name] = ConditionStarted
		}
		return dependencies, nil
	}
	err := parser.mapping(node, field, func(name string, value *yaml.Node, nameField string) error {
		condition := ConditionStarted
		err := parser.mapping(value, nameField, func(key string, option *yaml.Node, optionField string) error {
			if key != "condition" {
				return specError("%s: not supported", optionField)
			}
			var err error
			condition, err = scalar(option, optionField)
			return err
		})
		dependencies[name] = condition
		return err
	})
	return dependencies, err
}

func (parser composeParser) deploy(node *yaml.Node, field string, spec *ContainerSpec) error {
	return parser.mapping(node, field, func(key string, value *yaml.Node, keyField string) error {
		if key != "resources" {
			return specError("%s: not supported", keyField)
		}
		return parser.mapping(value, keyField, func(section string, sectionNode *yaml.Node, sectionField string) error {
			switch section {
			case "limits":
				return parser.mapping(sectionNode, sectionField, func(limit string, limitNode *yaml.Node, limitField string) error {
					var err error
					switch limit {
					case "memory":
						spec.MemoryLimitMB, err = megabytesValue(limitNode, limitField)
					case "cpus":
						var text string
						text, err = scalar(limitNode, limitField)
						if err == nil {
							spec.CPULimit, err = strconv.ParseFloat(text, 64)
							if err != nil {
								err = specError("%s: must be a number", limitField)
							}
						}
					default:
						err = specError("%s: not supported", limitField)
					}
					return err
				})
			case "reservations":
				return parser.mapping(sectionNode, sectionField, func(reservation string, reservationNode *yaml.Node, reservationField string) error {
					if reservation != "devices" {
						return specError("%s: not supported", reservationField)
					}
					count, err := gpuDevices(reservationNode, reservationField)
					spec.GPUs = count
					return err
				})
			default:
				return specError("%s: not supported", sectionField)
			}
		})
	})
}

// gpuDevices reads device reservations: only nvidia GPUs with a count.
func gpuDevices(node *yaml.Node, field string) (int, error) {
	if node.Kind != yaml.SequenceNode {
		return 0, specError("%s: must be a list", field)
	}
	total := 0
	for index, device := range node.Content {
		deviceField := fmt.Sprintf("%s[%d]", field, index)
		if device.Kind != yaml.MappingNode {
			return 0, specError("%s: must be a mapping", deviceField)
		}
		count, gpu := 0, false
		for position := 0; position+1 < len(device.Content); position += 2 {
			key, value := device.Content[position].Value, device.Content[position+1]
			keyField := deviceField + "." + key
			switch key {
			case "driver":
				if value.Value != "nvidia" {
					return 0, specError("%s: only driver nvidia is supported", keyField)
				}
			case "count":
				if value.Value == "all" {
					return 0, specError("%s: count all is not supported, give a number", keyField)
				}
				parsed, err := strconv.Atoi(value.Value)
				if err != nil || parsed < 1 {
					return 0, specError("%s: must be a positive whole number", keyField)
				}
				count = parsed
			case "capabilities":
				capabilities, err := stringList(value, keyField)
				if err != nil {
					return 0, err
				}
				for _, capability := range capabilities {
					if capability == "gpu" {
						gpu = true
					}
				}
			default:
				return 0, specError("%s: not supported", keyField)
			}
		}
		if !gpu || count == 0 {
			return 0, specError("%s: needs capabilities [gpu] and a count", deviceField)
		}
		total += count
	}
	return total, nil
}

// megabytesValue parses byte sizes such as 512m, 16g or 1gb into whole
// megabytes, rounding up.
func megabytesValue(node *yaml.Node, field string) (int, error) {
	text, err := scalar(node, field)
	if err != nil {
		return 0, err
	}
	lower := strings.ToLower(strings.TrimSpace(text))
	lower = strings.TrimSuffix(lower, "b")
	multiplier := 1.0
	switch {
	case strings.HasSuffix(lower, "k"):
		multiplier, lower = 1.0/1024, strings.TrimSuffix(lower, "k")
	case strings.HasSuffix(lower, "m"):
		multiplier, lower = 1, strings.TrimSuffix(lower, "m")
	case strings.HasSuffix(lower, "g"):
		multiplier, lower = 1024, strings.TrimSuffix(lower, "g")
	default:
		multiplier = 1.0 / (1024 * 1024)
	}
	number, err := strconv.ParseFloat(lower, 64)
	if err != nil || number <= 0 {
		return 0, specError("%s: %q is not a size such as 512m or 2g", field, text)
	}
	return int(math.Ceil(number * multiplier)), nil
}

func restartValue(node *yaml.Node, field string) (string, error) {
	text, err := scalar(node, field)
	if err != nil {
		return "", err
	}
	switch {
	case text == "no":
		return RestartNo, nil
	case text == "on-failure" || strings.HasPrefix(text, "on-failure:"):
		return RestartOnFailure, nil
	default:
		return "", specError("%s: %q is not supported, use no or on-failure", field, text)
	}
}

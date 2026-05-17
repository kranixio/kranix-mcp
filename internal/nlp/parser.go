package nlp

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// DeploymentIntent represents a parsed natural language deployment request
type DeploymentIntent struct {
	Action     string                 `json:"action"`     // deploy, update, scale
	Name       string                 `json:"name"`       // workload name
	Version    string                 `json:"version"`    // version tag
	Image      string                 `json:"image"`      // full image reference
	Namespace  string                 `json:"namespace"`  // target namespace
	Replicas   int                    `json:"replicas"`   // replica count
	Env        map[string]string      `json:"env"`        // environment variables
	Raw        string                 `json:"raw"`        // original input
	Confidence float64                `json:"confidence"` // parsing confidence
	Metadata   map[string]interface{} `json:"metadata"`   // additional metadata
}

// Parser handles natural language parsing for deployment commands
type Parser struct {
	// Common patterns
	deployPattern    *regexp.Regexp
	updatePattern    *regexp.Regexp
	scalePattern     *regexp.Regexp
	namespacePattern *regexp.Regexp
	replicaPattern   *regexp.Regexp
	envPattern       *regexp.Regexp
	versionPattern   *regexp.Regexp
}

func NewParser() *Parser {
	return &Parser{
		deployPattern:    regexp.MustCompile(`(?i)(deploy|create|spin up|launch)\s+(?:the\s+)?(?:workload|app|application|service|container)?\s*"?([\w-]+)"?`),
		updatePattern:    regexp.MustCompile(`(?i)(update|upgrade|change)\s+(?:the\s+)?(?:workload|app|application|service)?\s*"?([\w-]+)"?`),
		scalePattern:     regexp.MustCompile(`(?i)(scale\s+(?:up|down)?|set\s+replicas?|change\s+replicas?)`),
		namespacePattern: regexp.MustCompile(`(?i)(?:to|in|for|namespace)\s+"?([\w-]+)"?`),
		replicaPattern:   regexp.MustCompile(`(?i)(\d+)\s+replicas?`),
		envPattern:       regexp.MustCompile(`(?i)(?:with|set|env|environment)\s+([\w-]+)\s*=\s*"?([\w-./]+)"?`),
		versionPattern:   regexp.MustCompile(`(?i)(?:version|v|tag)\s*[:=]?\s*"?([\w.]+)"?`),
	}
}

// ParseDeploymentIntent parses a natural language command into a structured deployment intent
func (p *Parser) ParseDeploymentIntent(input string) (*DeploymentIntent, error) {
	intent := &DeploymentIntent{
		Raw:      input,
		Env:      make(map[string]string),
		Metadata: make(map[string]interface{}),
		Replicas: 1,
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("empty input")
	}

	// Detect action type
	if p.deployPattern.MatchString(input) {
		intent.Action = "deploy"
		matches := p.deployPattern.FindStringSubmatch(input)
		if len(matches) > 2 {
			intent.Name = matches[2]
		}
	} else if p.updatePattern.MatchString(input) {
		intent.Action = "update"
		matches := p.updatePattern.FindStringSubmatch(input)
		if len(matches) > 2 {
			intent.Name = matches[2]
		}
	} else if p.scalePattern.MatchString(input) {
		intent.Action = "scale"
	} else {
		// Default to deploy if no action specified
		intent.Action = "deploy"
	}

	// Extract workload name if not already set
	if intent.Name == "" {
		intent.Name = p.extractWorkloadName(input)
	}

	// Extract namespace
	intent.Namespace = p.extractNamespace(input)
	if intent.Namespace == "" {
		intent.Namespace = "default"
	}

	// Extract replicas
	intent.Replicas = p.extractReplicas(input)

	// Extract version
	intent.Version = p.extractVersion(input)

	// Construct image reference
	intent.Image = p.constructImage(intent.Name, intent.Version, input)

	// Extract environment variables
	intent.Env = p.extractEnvVars(input)

	// Calculate confidence score
	intent.Confidence = p.calculateConfidence(intent)

	return intent, nil
}

func (p *Parser) extractWorkloadName(input string) string {
	// Try to find a word that looks like a workload name
	words := strings.Fields(input)
	for i, word := range words {
		// Skip common verbs and prepositions
		if p.isCommonWord(word) {
			continue
		}
		// Look for quoted strings
		if strings.HasPrefix(word, `"`) || strings.HasPrefix(word, "'") {
			return strings.Trim(word, `"'"`)
		}
		// Take the next non-common word as the name
		if i < len(words)-1 && !p.isCommonWord(words[i+1]) {
			return words[i+1]
		}
	}
	return "app"
}

func (p *Parser) isCommonWord(word string) bool {
	commonWords := []string{
		"deploy", "create", "spin", "up", "launch", "the", "a", "an",
		"workload", "app", "application", "service", "container", "to", "in",
		"for", "with", "version", "v", "tag", "replicas", "scale", "update",
		"upgrade", "change", "set", "environment", "env", "namespace", "ns",
	}
	lower := strings.ToLower(word)
	for _, cw := range commonWords {
		if lower == cw {
			return true
		}
	}
	return false
}

func (p *Parser) extractNamespace(input string) string {
	matches := p.namespacePattern.FindStringSubmatch(input)
	if len(matches) > 1 {
		return matches[1]
	}

	// Common namespace mappings
	lower := strings.ToLower(input)
	if strings.Contains(lower, "prod") || strings.Contains(lower, "production") {
		return "production"
	}
	if strings.Contains(lower, "staging") || strings.Contains(lower, "stage") {
		return "staging"
	}
	if strings.Contains(lower, "dev") || strings.Contains(lower, "develop") {
		return "development"
	}
	if strings.Contains(lower, "test") {
		return "test"
	}

	return ""
}

func (p *Parser) extractReplicas(input string) int {
	matches := p.replicaPattern.FindStringSubmatch(input)
	if len(matches) > 1 {
		if count, err := strconv.Atoi(matches[1]); err == nil {
			return count
		}
	}
	return 1
}

func (p *Parser) extractVersion(input string) string {
	matches := p.versionPattern.FindStringSubmatch(input)
	if len(matches) > 1 {
		return matches[1]
	}

	// Look for version patterns like "v2" or "2.0"
	versionPatterns := []string{
		`v(\d+(?:\.\d+)*)`,
		`(\d+\.\d+\.\d+)`,
		`(\d+\.\d+)`,
	}
	for _, pattern := range versionPatterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(input); len(matches) > 1 {
			return matches[1]
		}
	}

	return "latest"
}

func (p *Parser) constructImage(name, version, input string) string {
	// Check if input already contains an image reference
	imagePattern := regexp.MustCompile(`[\w.-]+/[\w.-]+(?::[\w.-]+)?`)
	if match := imagePattern.FindString(input); match != "" {
		return match
	}

	// Construct image from name and version
	if version == "" || version == "latest" {
		return fmt.Sprintf("%s:latest", name)
	}
	return fmt.Sprintf("%s:%s", name, version)
}

func (p *Parser) extractEnvVars(input string) map[string]string {
	env := make(map[string]string)
	matches := p.envPattern.FindAllStringSubmatch(input, -1)
	for _, match := range matches {
		if len(match) > 2 {
			env[match[1]] = match[2]
		}
	}
	return env
}

func (p *Parser) calculateConfidence(intent *DeploymentIntent) float64 {
	score := 0.5 // Base confidence

	if intent.Name != "" && intent.Name != "app" {
		score += 0.2
	}
	if intent.Namespace != "" && intent.Namespace != "default" {
		score += 0.1
	}
	if intent.Replicas > 1 {
		score += 0.1
	}
	if intent.Version != "" && intent.Version != "latest" {
		score += 0.1
	}

	if score > 1.0 {
		score = 1.0
	}

	return score
}

// ValidateIntent checks if the parsed intent is valid
func (p *Parser) ValidateIntent(intent *DeploymentIntent) []string {
	var errors []string

	if intent.Name == "" || intent.Name == "app" {
		errors = append(errors, "workload name could not be determined")
	}
	if intent.Image == "" {
		errors = append(errors, "image reference could not be determined")
	}
	if intent.Namespace == "" {
		errors = append(errors, "namespace could not be determined")
	}
	if intent.Replicas < 1 {
		errors = append(errors, "replicas must be at least 1")
	}

	return errors
}

// FormatAsJSON returns the intent as a JSON string
func (p *Parser) FormatAsJSON(intent *DeploymentIntent) (string, error) {
	data, err := json.MarshalIndent(intent, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Examples returns example natural language commands
func (p *Parser) Examples() []string {
	return []string{
		`deploy api-server v2 to prod with 5 replicas`,
		`deploy "my-app" to staging with 3 replicas`,
		`scale workload frontend to 10 replicas in production`,
		`update backend-service to version 1.5.0 in development`,
		`deploy nginx:latest to default with env PORT=8080`,
		`create workload "auth-service" in namespace auth with 2 replicas`,
		`launch database service to prod with version v3.2.1`,
	}
}

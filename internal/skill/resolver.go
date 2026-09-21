package skill

import "fmt"

// ResolvedMode represents the determined action mode for skill generation
type ResolvedMode int

const (
	// ModeRepository indicates a new Repository Skill should be generated
	ModeRepository ResolvedMode = iota
	// ModeExplorer indicates an Explorer Skill should be added
	ModeExplorer
	// ModeAlreadyExists indicates an Ark skill already exists
	ModeAlreadyExists
	// ModeError indicates detection failed
	ModeError
)

// String returns a human-readable name for the mode
func (m ResolvedMode) String() string {
	switch m {
	case ModeRepository:
		return "repository"
	case ModeExplorer:
		return "explorer"
	case ModeAlreadyExists:
		return "already-exists"
	case ModeError:
		return "error"
	default:
		return "unknown"
	}
}

// ResolutionResult holds the resolved mode and related context
type ResolutionResult struct {
	Mode           ResolvedMode
	Detection      *DetectionResult
	ExistingArk    *DetectedSkill // Set when ModeAlreadyExists
	Message        string         // Human-readable explanation
	SuggestedName  string         // Suggested skill name
	SuggestedPath  string         // Suggested output path
}

// Resolver determines the appropriate skill generation mode
type Resolver struct {
	detector *Detector
	rootDir  string
}

// NewResolver creates a new mode resolver
func NewResolver(rootDir string) *Resolver {
	return &Resolver{
		detector: NewDetector(rootDir),
		rootDir:  rootDir,
	}
}

// Resolve analyzes the repository and determines the appropriate mode
func (r *Resolver) Resolve() (*ResolutionResult, error) {
	detection, err := r.detector.Detect()
	if err != nil {
		return &ResolutionResult{
			Mode:    ModeError,
			Message: fmt.Sprintf("Failed to detect skills: %v", err),
		}, err
	}

	result := &ResolutionResult{
		Detection: detection,
	}

	// Check if Ark skill already exists
	if detection.HasArkSkills() {
		arkSkill := detection.ArkSkills[0]
		result.Mode = ModeAlreadyExists
		result.ExistingArk = &arkSkill
		result.Message = fmt.Sprintf("Ark-generated skill already exists: %s", arkSkill.Name)
		return result, nil
	}

	// Determine mode based on existing skills
	if detection.HasUserSkills() {
		// User skills exist → add Explorer Skill
		result.Mode = ModeExplorer
		result.SuggestedName = "ark-code-explorer"
		result.SuggestedPath = r.suggestPath(detection, "ark-code-explorer")
		result.Message = fmt.Sprintf(
			"Existing skills detected (%d). Will generate Explorer Skill.",
			len(detection.UserSkills),
		)
	} else {
		// No skills exist → create Repository Skill
		result.Mode = ModeRepository
		result.SuggestedName = "repository-development"
		result.SuggestedPath = r.suggestPath(detection, "repository-development")
		result.Message = "No existing skills detected. Will generate Repository Skill."
	}

	return result, nil
}

// suggestPath determines the output path for a new skill
func (r *Resolver) suggestPath(detection *DetectionResult, skillName string) string {
	skillsDir := r.detector.GetDefaultSkillsDir()
	return fmt.Sprintf("%s/%s", skillsDir, skillName)
}

// ResolveForCommand determines mode for a specific command
func (r *Resolver) ResolveForCommand(command string) (*ResolutionResult, error) {
	detection, err := r.detector.Detect()
	if err != nil {
		return &ResolutionResult{
			Mode:    ModeError,
			Message: fmt.Sprintf("Failed to detect skills: %v", err),
		}, err
	}

	result := &ResolutionResult{
		Detection: detection,
	}

	switch command {
	case "init":
		// Force Repository Skill generation
		if detection.HasArkSkills() {
			for _, s := range detection.ArkSkills {
				if s.SkillType == SkillTypeRepository {
					result.Mode = ModeAlreadyExists
					result.ExistingArk = &s
					result.Message = "Repository Skill already exists"
					return result, nil
				}
			}
		}
		result.Mode = ModeRepository
		result.SuggestedName = "repository-development"
		result.SuggestedPath = r.suggestPath(detection, "repository-development")
		result.Message = "Will generate Repository Skill."

	case "add-explorer":
		// Force Explorer Skill generation
		if detection.HasArkSkills() {
			for _, s := range detection.ArkSkills {
				if s.SkillType == SkillTypeExplorer {
					result.Mode = ModeAlreadyExists
					result.ExistingArk = &s
					result.Message = "Explorer Skill already exists"
					return result, nil
				}
			}
		}
		result.Mode = ModeExplorer
		result.SuggestedName = "ark-code-explorer"
		result.SuggestedPath = r.suggestPath(detection, "ark-code-explorer")
		result.Message = "Will generate Explorer Skill."

	default:
		// Default auto mode
		return r.Resolve()
	}

	return result, nil
}

// PrintDetectionSummary returns a formatted summary of detected skills
func PrintDetectionSummary(detection *DetectionResult) string {
	if !detection.HasSkills() {
		return "No existing repository skills detected."
	}

	var sb fmt.Stringer = &detectionSummaryBuilder{detection: detection}
	return sb.String()
}

type detectionSummaryBuilder struct {
	detection *DetectionResult
}

func (b *detectionSummaryBuilder) String() string {
	var lines []string
	lines = append(lines, "Existing skills detected:")

	for _, skill := range b.detection.Skills {
		marker := "  "
		if skill.IsArkOwned {
			marker = "⚙ "
		}
		lines = append(lines, fmt.Sprintf("  %s%s", marker, skill.Name))
	}

	return fmt.Sprintf("%s\n", joinLines(lines))
}

func joinLines(lines []string) string {
	result := ""
	for i, line := range lines {
		if i > 0 {
			result += "\n"
		}
		result += line
	}
	return result
}

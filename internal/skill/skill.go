// Package skill embeds the MiniMax h3-prompt-writing skill so the binary works
// without any files on disk.
//
// Source: https://github.com/MiniMax-AI/MiniMax-H3/tree/main/skills/h3-prompt-writing
package skill

import (
	_ "embed"
)

//go:embed assets/SKILL.md
var skillMD string

//go:embed assets/references/base-en.txt
var baseGuide string

//go:embed assets/references/ref-en.txt
var refGuide string

// SkillMD is the skill front matter and workflow overview.
func SkillMD() string { return skillMD }

// BaseGuide is the T2VA / I2VA / FL2VA / L2VA writing guide.
func BaseGuide() string { return baseGuide }

// RefGuide is the full-reference (Ref2VA) rewrite format guide.
func RefGuide() string { return refGuide }

// GuideFor returns the reference guide that applies to an input mode.
func GuideFor(mode string) string {
	if mode == "Ref2VA" {
		return refGuide
	}
	return baseGuide
}

// IsRefMode reports whether a mode uses the six-section full-reference format.
func IsRefMode(mode string) bool { return mode == "Ref2VA" }

// Fields lists the output section names for a mode, in required order.
func Fields(mode string) []string {
	if IsRefMode(mode) {
		return []string{
			"subject_definitions",
			"summary",
			"retention_analysis",
			"detailed_description",
			"overall_soundscape",
			"non_diegetic_music",
		}
	}
	return []string{
		"integrated_multimodal_description",
		"overall_soundscape",
		"non_diegetic_music",
	}
}

// RetentionMarkers are the fixed relationship values allowed for visible
// content in retention_analysis.
var RetentionMarkers = []string{"fully_preserved", "partially_preserved", "attribute_transfer", "weak_reference"}

// AudioMarkers are the fixed relationship values allowed for <Audio N>.
var AudioMarkers = []string{"fully_copy", "partially_copy", "reference", "weak_reference"}

// TaskTypes are the summary prefixes allowed in full-reference mode.
var TaskTypes = []string{
	"keyframe completion",
	"reference generation",
	"video editing",
	"video continuation",
	"audio reuse",
	"audio reference",
}

// CameraMotions is the controlled vocabulary for camera movement.
var CameraMotions = []string{
	"Zoom In", "Zoom Out", "Push In", "Pull Out",
	"Pan Left", "Pan Right", "Truck Left", "Truck Right",
	"Tilt Up", "Tilt Down", "Pedestal Up", "Pedestal Down",
	"Arc Shot", "Tracking Shot", "Static Shot",
	"Shake Slightly", "Shake Strongly", "POV",
	"Roll Clockwise", "Roll Counterclockwise",
}

// Styles are the visual styles the guide names explicitly.
var Styles = []string{
	"Cinematic", "live-action", "2D-animated", "3D CG",
	"claymation", "watercolor", "vintage film",
}

// VagueWords are the abstract terms the guide tells writers to avoid.
var VagueWords = []string{
	"cinematic feel", "beautiful", "stunning", "gorgeous", "epic",
	"emotional", "atmospheric", "moody", "breathtaking", "masterpiece",
	"high quality", "best quality", "4k", "8k", "ultra detailed",
	"award winning", "dramatic lighting", "vibe",
}

package validate

import (
	"strings"
	"testing"
)

// The four base-mode cases and the full-reference case below are copied
// verbatim from the h3-prompt-writing skill's own reference guides. If the
// validator rejects the guide's own examples, the rules are wrong.

const caseT2VA = `integrated_multimodal_description: [Shot 1] Live-action, cinematic, a medium-wide shot frames a baker opening the shutters of a small street bakery before sunrise. The camera pushes in with small amplitude at slow speed as the middle-aged baker with a calm, slightly raspy voice (S1) places a fresh loaf on the wooden counter and says: <d>[English] First batch of the morning.</d> [Shot 2] At 00:05.000, the camera cuts to a close-up of steam rising from the sliced bread while the baker's final words carry over from the previous shot.

overall_soundscape: Wooden shutters scrape open over a quiet street as trays clink softly inside the bakery. The doorbell rings once, followed by light footsteps and the crisp sound of bread being sliced.

non_diegetic_music: A soft acoustic-guitar pattern at a moderate tempo, joined by sparse upright-bass notes and a gentle fade at the end.`

const caseI2VA = `For the target video, at 0.00 seconds into the target video, <Picture 1> (from [Shot 1]) is fully referenced.

integrated_multimodal_description: [Shot 1] Live-action, cinematic, the young woman shown in <Picture 1> remains beside the rain-covered train window, preserving her appearance, clothing, seat position, and the carriage layout. The camera trucks right with small amplitude at slow speed as she lifts her gaze from the folded letter toward the passing city lights. Her reflection moves across the glass while the quiet, breathy young woman (S1) says: <d>[English] I get off at the next station.</d> She folds the letter along its existing crease.

overall_soundscape: The train wheels produce a steady metallic rhythm beneath a low ventilation hum. Rain ticks against the window while paper rustles softly in her hands.

non_diegetic_music: Sustained cello notes at a slow tempo with widely spaced piano tones, gradually decreasing in volume.`

const caseFL2VA = `How the reference pictures align with the target video — Picture 1 (from Shot 1) aligns with the 0.00-second mark of the target video; Picture 2 (from Shot 1) aligns with the 8.00-second mark of the target video.

integrated_multimodal_description: [Shot 1] Live-action, cinematic, a rain-soaked cyclist begins in the position and framing established by Picture 1, holding a closed black umbrella beside a silver bicycle. The camera pulls out with small amplitude at slow speed as she releases the bicycle handle, raises the umbrella above her shoulder, and presses the runner upward until the canopy opens. Water rolls from the expanding fabric while she steps beneath it, rotates the handle into the final angle, and settles into the pose, spacing, and composition established by Picture 2 at the end of the shot.

overall_soundscape: Rain falls steadily on the pavement, followed by the metallic click of the umbrella runner and the soft snap of the canopy opening. Water drips from the bicycle frame as distant traffic passes.

non_diegetic_music: N/A`

const caseL2VA = `How the reference pictures align with the target video — <Picture 1> (from [Shot 1]) aligns with the 6.00-second mark of the target video.

integrated_multimodal_description: [Shot 1] Live-action, cinematic, a close shot begins with an intact drinking glass near the edge of a dark wooden table, while the same hand and sleeve visible in <Picture 1> approach from the right. The camera pushes in with small amplitude at slow speed as the fingertips strike the rim. The glass tips, falls, and hits the floor with a sharp impact; cracks spread through it as fragments slide outward. Toward the end, the moving pieces lose momentum and settle into the exact broken arrangement, hand position, camera angle, lighting, and final composition established by <Picture 1>.

overall_soundscape: Fingertips tap the glass before it scrapes across the tabletop, falls, and breaks with a sharp crash. Small fragments scatter and gradually stop sliding across the floor.

non_diegetic_music: A low electronic pulse at a slow tempo, ending immediately after the glass breaks.`

const caseRef2VA = `subject_definitions:
<Subject 1> is the coffee-shop environment in <Picture 1>, featuring an exposed brick wall, an orange tufted sofa with patterned pillows, a neon sign, and a wooden coffee table.
<Subject 2> is the fluffy white Samoyed in <Picture 2>, <Picture 3>, and <Picture 4>, with thick white fur, pointed ears, a dark nose, and a curved tail.
<Subject 3> is the young blonde woman in <Video 1>, with long blonde hair and a light-pink button-down shirt with rolled-up sleeves.
<Subject 4> is the young man in <Video 2>, with short wavy brown hair and a dark-grey hoodie with drawstrings.
<Audio 1> is the voice-timbre reference for <Subject 3> (S1), containing a spoken English vocal layer.

summary:
[reference generation + audio reference] The target video shows <Subject 3> eating a cookie in <Subject 1>. <Subject 4> enters with <Subject 2>, which lunges toward the cookie. The three-shot exchange uses <Audio 1> as the voice-timbre reference for <Subject 3> and ends with a canned audience laugh.

retention_analysis:
<Subject 1> (appears in [Shot 1], [Shot 2], [Shot 3]): fully_preserved - the exposed brick wall, orange tufted sofa, patterned pillows, neon sign, and wooden coffee table are retained.
<Subject 2> (appears in [Shot 1], [Shot 2]): fully_preserved - the Samoyed's thick white fur, pointed ears, dark nose, and curved tail are retained.
<Subject 3> (appears in [Shot 1], [Shot 2], [Shot 3]): fully_preserved - the blonde woman's identity, long hair, and light-pink shirt are retained.
<Subject 4> (appears in [Shot 1], [Shot 2]): fully_preserved - the young man's short wavy brown hair and dark-grey hoodie are retained.
<Audio 1>: reference - its vocal timbre guides the dialogue delivery of <Subject 3> without copying the original signal.

detailed_description:
The target video uses a realistic multi-camera sitcom style with warm indoor lighting.
[Shot 1] A medium shot establishes <Subject 1>, the coffee shop with its exposed brick wall, orange tufted sofa, patterned pillows, neon sign, and wooden coffee table. <Subject 3> (S1), the young woman with long blonde hair and a light-pink button-down shirt with rolled-up sleeves, sits on the sofa holding a chocolate-chip cookie. From the left, <Subject 4>, the young man with short wavy brown hair and a dark-grey hoodie with drawstrings, enters holding the leash of <Subject 2>, the thick-furred white Samoyed with pointed ears, a dark nose, and a curved tail. The dog lunges toward the cookie and pulls the leash taut. <Subject 3> (S1) jerks her hand back and, using the clear youthful voice timbre referenced from <Audio 1>, exclaims with light annoyance, <d>[English] Hey! Watch your dog!</d> She closes her lips and guards the cookie while <Subject 4> pulls the dog back.
[Shot 2] At 00:03.000, the shot cuts to a close-up of <Subject 4> (S2), the young man in the dark-grey hoodie from Shot 1, sitting beside <Subject 3> on the sofa and holding <Subject 2> securely in his arms. <Subject 4> (S2) says in a casual young male voice with a playful tone and an easy conversational pace, <d>[English] He just likes cookies more than me.</d> He closes his mouth into an apologetic smile and strokes the dog's thick white fur.
[Shot 3] At 00:05.000, the shot cuts to a close-up of <Subject 3> (S1), the blonde woman in the light-pink shirt from Shot 1. Her annoyance softens as she looks toward the Samoyed. <Subject 3> (S1) replies in the same clear youthful voice referenced from <Audio 1> with an amused cadence, <d>[English] Well, he has good taste at least.</d> She smiles and raises the cookie in a small toast-like gesture. A classic canned audience laugh begins immediately after the line and continues through the final frame.

overall_soundscape:
Soft indoor coffee-shop room tone continues throughout the scene.

non_diegetic_music:
N/A`

func errorsOnly(findings []Finding) []Finding {
	var out []Finding
	for _, f := range findings {
		if f.Severity == SeverityError {
			out = append(out, f)
		}
	}
	return out
}

func describe(findings []Finding) string {
	var b strings.Builder
	for _, f := range findings {
		b.WriteString("\n  [" + f.Rule + "] " + f.Message)
		if f.Detail != "" {
			b.WriteString(" — " + f.Detail)
		}
	}
	return b.String()
}

func TestGuideExamplesPass(t *testing.T) {
	cases := []struct {
		name   string
		prompt string
		spec   Spec
	}{
		{
			name:   "T2VA",
			prompt: caseT2VA,
			spec:   Spec{Mode: "T2VA", Duration: 6, StrictEnglish: true},
		},
		{
			name:   "I2VA",
			prompt: caseI2VA,
			spec: Spec{
				Mode: "I2VA", Duration: 5.17, StrictEnglish: true,
				PictureLabels: []string{"<Picture 1>"},
				Dialogue:      []string{"I get off at the next station."},
			},
		},
		{
			name:   "FL2VA",
			prompt: caseFL2VA,
			spec: Spec{
				Mode: "FL2VA", Duration: 8, StrictEnglish: true,
				PictureLabels: []string{"<Picture 1>", "<Picture 2>"},
			},
		},
		{
			name:   "L2VA",
			prompt: caseL2VA,
			spec: Spec{
				Mode: "L2VA", Duration: 6, StrictEnglish: true,
				PictureLabels: []string{"<Picture 1>"},
			},
		},
		{
			name:   "Ref2VA",
			prompt: caseRef2VA,
			spec: Spec{
				Mode: "Ref2VA", Duration: 6, StrictEnglish: true,
				PictureLabels: []string{"<Picture 1>", "<Picture 2>", "<Picture 3>", "<Picture 4>"},
				VideoLabels:   []string{"<Video 1>", "<Video 2>"},
				AudioLabels:   []string{"<Audio 1>"},
				Dialogue: []string{
					"Hey! Watch your dog!",
					"He just likes cookies more than me.",
					"Well, he has good taste at least.",
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := Check(tc.prompt, tc.spec)
			errs := errorsOnly(findings)
			if len(errs) > 0 {
				t.Errorf("guide example %s produced %d blocking finding(s):%s",
					tc.name, len(errs), describe(errs))
			}
			// The FL2VA example refers to its keyframes as "Picture 1" without
			// angle brackets, which must still count as using the label.
			if tc.name == "FL2VA" && hasRule(findings, "label.unused") {
				t.Errorf("bare Picture N references should count as label use:%s",
					describe(findings))
			}
		})
	}
}

func hasRule(findings []Finding, rule string) bool {
	for _, f := range findings {
		if f.Rule == rule {
			return true
		}
	}
	return false
}

func TestCatchesFormatViolations(t *testing.T) {
	base := Spec{
		Mode: "I2VA", Duration: 5.17, StrictEnglish: true,
		PictureLabels: []string{"<Picture 1>"},
	}

	t.Run("cut beyond the workflow duration", func(t *testing.T) {
		prompt := strings.Replace(caseI2VA,
			"She folds the letter along its existing crease.",
			"[Shot 2] At 00:09.000, the camera cuts to the platform.", 1)
		if !hasRule(Check(prompt, base), "shot.overrun") {
			t.Error("expected shot.overrun for a cut past the end of the video")
		}
	})

	t.Run("timestamp on the first shot", func(t *testing.T) {
		prompt := strings.Replace(caseI2VA, "[Shot 1] Live-action",
			"[Shot 1] At 00:00.500, Live-action", 1)
		if !hasRule(Check(prompt, base), "shot.firstTimestamp") {
			t.Error("expected shot.firstTimestamp")
		}
	})

	t.Run("undeclared reference label", func(t *testing.T) {
		prompt := strings.Replace(caseI2VA, "<Picture 1> remains beside",
			"<Picture 3> remains beside", 1)
		if !hasRule(Check(prompt, base), "label.undeclared") {
			t.Error("expected label.undeclared for a label the workflow does not wire")
		}
	})

	t.Run("chinese leaking into the body", func(t *testing.T) {
		prompt := strings.Replace(caseI2VA, "the young woman shown in",
			"画面里的年轻女子出现在", 1)
		findings := Check(prompt, base)
		if !hasRule(findings, "language.cjk") {
			t.Error("expected language.cjk for non-English body text")
		}
		relaxed := base
		relaxed.StrictEnglish = false
		for _, f := range Check(prompt, relaxed) {
			if f.Rule == "language.cjk" && f.Severity == SeverityError {
				t.Error("language.cjk should be a warning when strict mode is off")
			}
		}
	})

	t.Run("dialogue kept in quotes stays legal", func(t *testing.T) {
		prompt := strings.Replace(caseI2VA, "the passing city lights",
			`a red neon sign reading "营业中"`, 1)
		for _, f := range Check(prompt, base) {
			if f.Rule == "language.cjk" {
				t.Error("screen text inside double quotes must not be flagged")
			}
		}
	})

	t.Run("rewritten dialogue", func(t *testing.T) {
		spec := base
		spec.Dialogue = []string{"I get off at the next stop."}
		if !hasRule(Check(caseI2VA, spec), "dialogue.verbatim") {
			t.Error("expected dialogue.verbatim when the model paraphrases a line")
		}
	})

	t.Run("missing field", func(t *testing.T) {
		prompt := strings.Replace(caseI2VA, "non_diegetic_music:", "background_music:", 1)
		if !hasRule(Check(prompt, base), "field.missing") {
			t.Error("expected field.missing")
		}
	})

	t.Run("voiceover without closed lips", func(t *testing.T) {
		prompt := strings.Replace(caseI2VA, "(S1) says:",
			"(S1) says in an off-screen voiceover:", 1)
		if !hasRule(Check(prompt, base), "dialogue.voiceover") {
			t.Error("expected dialogue.voiceover")
		}
	})

	t.Run("unbalanced dialogue tags", func(t *testing.T) {
		prompt := strings.Replace(caseI2VA, "next station.</d>", "next station.", 1)
		if !hasRule(Check(prompt, base), "dialogue.unbalanced") {
			t.Error("expected dialogue.unbalanced")
		}
	})
}

func TestCatchesRefModeViolations(t *testing.T) {
	spec := Spec{
		Mode: "Ref2VA", Duration: 6, StrictEnglish: true,
		PictureLabels: []string{"<Picture 1>", "<Picture 2>", "<Picture 3>", "<Picture 4>"},
		VideoLabels:   []string{"<Video 1>", "<Video 2>"},
		AudioLabels:   []string{"<Audio 1>"},
	}

	t.Run("invalid retention marker", func(t *testing.T) {
		prompt := strings.Replace(caseRef2VA,
			"<Subject 1> (appears in [Shot 1], [Shot 2], [Shot 3]): fully_preserved",
			"<Subject 1> (appears in [Shot 1], [Shot 2], [Shot 3]): mostly_kept", 1)
		if !hasRule(Check(prompt, spec), "ref.marker") {
			t.Error("expected ref.marker for a value outside the fixed vocabulary")
		}
	})

	t.Run("invalid task type", func(t *testing.T) {
		prompt := strings.Replace(caseRef2VA, "[reference generation + audio reference]",
			"[style transfer]", 1)
		if !hasRule(Check(prompt, spec), "ref.taskType") {
			t.Error("expected ref.taskType")
		}
	})

	t.Run("speaker id inside retention_analysis", func(t *testing.T) {
		prompt := strings.Replace(caseRef2VA,
			"<Audio 1>: reference - its vocal timbre",
			"<Audio 1>: reference - the (S1) vocal timbre", 1)
		if !hasRule(Check(prompt, spec), "speaker.retention") {
			t.Error("expected speaker.retention")
		}
	})

	t.Run("undefined subject label", func(t *testing.T) {
		prompt := strings.Replace(caseRef2VA,
			"[Shot 3] At 00:05.000, the shot cuts to a close-up of <Subject 3> (S1)",
			"[Shot 3] At 00:05.000, the shot cuts to a close-up of <Subject 5> (S1)", 1)
		findings := Check(prompt, spec)
		if !hasRule(findings, "ref.undefined") && !hasRule(findings, "label.subjectGap") {
			t.Error("expected an undefined-label finding")
		}
	})
}

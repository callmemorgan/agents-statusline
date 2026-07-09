package payload

import "testing"

func TestModelClassAccessorsReadModelScoped(t *testing.T) {
	pctF, pctS := 67.0, 22.0
	reset := int64(1752148800)
	r := RateLimits{
		ModelScoped: []ModelScopedLimit{
			{DisplayName: "Fable", UsedPercentage: &pctF, ResetsAt: &reset},
			{DisplayName: "Claude Sonnet", UsedPercentage: &pctS},
			{DisplayName: "Opus"}, // no usage data → treated as absent
		},
	}
	f := r.Fable()
	if f.UsedPercentage == nil || *f.UsedPercentage != 67 {
		t.Errorf("Fable() = %v, want 67", f.UsedPercentage)
	}
	if f.ResetsAt == nil || *f.ResetsAt != reset {
		t.Errorf("Fable resets_at = %v, want %d", f.ResetsAt, reset)
	}
	if s := r.Sonnet(); s.UsedPercentage == nil || *s.UsedPercentage != 22 {
		t.Errorf("Sonnet() = %v, want 22 (substring match)", s.UsedPercentage)
	}
	if r.Opus().UsedPercentage != nil {
		t.Errorf("Opus without data should be empty, got %v", r.Opus())
	}
}

// TestModelClassFieldsNotOnTheWire pins the intentional absence: Claude
// Code's statusline payload does not send model-class weekly windows, so the
// parser must ignore any such keys (they arrive via the quota shim instead).
func TestModelClassFieldsNotOnTheWire(t *testing.T) {
	raw := []byte(`{
		"model": {"display_name": "Claude Fable 5"},
		"workspace": {"current_dir": "~"},
		"rate_limits": {
			"five_hour": {"used_percentage": 10, "resets_at": 1},
			"seven_day_overage_included": {"used_percentage": 55, "resets_at": 2},
			"seven_day_sonnet": {"used_percentage": 12, "resets_at": 3},
			"model_scoped": [{"display_name": "Fable", "used_percentage": 99}]
		}
	}`)
	p := ParsePayload(raw)
	if got := p.RateLimits.FiveHour.UsedPercentage; got == nil || *got != 10 {
		t.Errorf("five_hour = %v, want 10", got)
	}
	if p.RateLimits.Fable().UsedPercentage != nil {
		t.Errorf("Fable parsed from the wire: %v, want ignored", p.RateLimits.Fable())
	}
	if p.RateLimits.Sonnet().UsedPercentage != nil {
		t.Errorf("Sonnet parsed from the wire: %v, want ignored", p.RateLimits.Sonnet())
	}
	if len(p.RateLimits.ModelScoped) != 0 {
		t.Errorf("model_scoped parsed from the wire: %v, want ignored", p.RateLimits.ModelScoped)
	}
}

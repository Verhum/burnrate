package usage

import (
	"encoding/json"
	"time"
)

type Window struct {
	Utilization float64   `json:"utilization"`
	ResetsAt    time.Time `json:"resets_at"`
}

type Limit struct {
	Kind       string  `json:"kind"`
	Group      string  `json:"group"`
	Percent    float64 `json:"percent"`
	Severity   string  `json:"severity"`
	IsActive   bool    `json:"is_active"`
	ResetsAt   string  `json:"resets_at"`
	ScopeModel string  `json:"scope_model,omitempty"`
}

type ScopedWeekly struct {
	Model   string  `json:"model"`
	Percent float64 `json:"percent"`
}

type Snapshot struct {
	FiveHour     Window
	SevenDay     Window
	SevenDayOpus *Window
	ScopedWeekly []ScopedWeekly
	Limits       []Limit
	Raw          json.RawMessage
	CapturedAt   time.Time
}

type apiResponse struct {
	FiveHour     *apiWindow `json:"five_hour"`
	SevenDay     *apiWindow `json:"seven_day"`
	SevenDayOpus *apiWindow `json:"seven_day_opus"`
	Limits       []apiLimit `json:"limits"`
}

type apiWindow struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    string  `json:"resets_at"`
}

type apiLimit struct {
	Kind     string  `json:"kind"`
	Group    string  `json:"group"`
	Percent  float64 `json:"percent"`
	Severity string  `json:"severity"`
	IsActive bool    `json:"is_active"`
	ResetsAt string  `json:"resets_at"`
	Scope    *struct {
		Model *struct {
			DisplayName string `json:"display_name"`
		} `json:"model"`
	} `json:"scope"`
}

func parseAPIResponse(data []byte) (Snapshot, error) {
	var resp apiResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return Snapshot{}, err
	}

	s := Snapshot{
		Raw:        json.RawMessage(data),
		CapturedAt: time.Now().UTC(),
	}

	if len(resp.Limits) > 0 {
		for _, l := range resp.Limits {
			lim := Limit{
				Kind:     l.Kind,
				Group:    l.Group,
				Percent:  l.Percent,
				Severity: l.Severity,
				IsActive: l.IsActive,
				ResetsAt: l.ResetsAt,
			}
			if l.Scope != nil && l.Scope.Model != nil {
				lim.ScopeModel = l.Scope.Model.DisplayName
			}
			s.Limits = append(s.Limits, lim)
		}

		for _, l := range resp.Limits {
			if l.Kind == "session" {
				t, _ := time.Parse(time.RFC3339, l.ResetsAt)
				if t.IsZero() {
					t, _ = parseFlexTime(l.ResetsAt)
				}
				s.FiveHour = Window{Utilization: l.Percent, ResetsAt: t}
			}
			if l.Kind == "weekly_all" {
				t, _ := time.Parse(time.RFC3339, l.ResetsAt)
				if t.IsZero() {
					t, _ = parseFlexTime(l.ResetsAt)
				}
				s.SevenDay = Window{Utilization: l.Percent, ResetsAt: t}
			}
			if l.Kind == "weekly_scoped" && l.Scope != nil && l.Scope.Model != nil {
				s.ScopedWeekly = append(s.ScopedWeekly, ScopedWeekly{
					Model:   l.Scope.Model.DisplayName,
					Percent: l.Percent,
				})
			}
		}
	} else {
		if resp.FiveHour != nil {
			s.FiveHour = parseWindow(resp.FiveHour)
		}
		if resp.SevenDay != nil {
			s.SevenDay = parseWindow(resp.SevenDay)
		}
	}

	if resp.SevenDayOpus != nil {
		w := parseWindow(resp.SevenDayOpus)
		s.SevenDayOpus = &w
	}

	return s, nil
}

func parseWindow(w *apiWindow) Window {
	t, _ := time.Parse(time.RFC3339, w.ResetsAt)
	if t.IsZero() {
		t, _ = parseFlexTime(w.ResetsAt)
	}
	return Window{
		Utilization: w.Utilization,
		ResetsAt:    t,
	}
}

func parseFlexTime(s string) (time.Time, error) {
	formats := []string{
		"2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02T15:04:05Z07:00",
		time.RFC3339Nano,
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, nil
}

package common

import "sort"

type SignalSeverity string

const (
	SeverityInfo    SignalSeverity = "info"
	SeverityActive  SignalSeverity = "active"
	SeveritySuccess SignalSeverity = "success"
	SeverityWarning SignalSeverity = "warning"
	SeverityUrgent  SignalSeverity = "urgent"
)

type Signal struct {
	Source   string         `json:"source"`
	Key      string         `json:"key"`
	Severity SignalSeverity `json:"severity"`
	Label    string         `json:"label"`
}

var severityRank = map[SignalSeverity]int{
	SeverityInfo:    0,
	SeverityActive:  1,
	SeveritySuccess: 2,
	SeverityWarning: 3,
	SeverityUrgent:  4,
}

func TopSeverity(signals []Signal) SignalSeverity {
	if len(signals) == 0 {
		return ""
	}
	top := signals[0].Severity
	for _, s := range signals[1:] {
		if severityRank[s.Severity] > severityRank[top] {
			top = s.Severity
		}
	}
	return top
}

func SortSignals(signals []Signal) {
	sort.SliceStable(signals, func(i, j int) bool {
		return severityRank[signals[i].Severity] > severityRank[signals[j].Severity]
	})
}

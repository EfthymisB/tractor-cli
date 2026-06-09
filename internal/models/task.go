package models

// Task represents a single task within a Tractor job.
type Task struct {
	TID       int     `json:"tid"`
	JID       int     `json:"jid"`
	Title     string  `json:"title"`
	Status    string  `json:"status"`
	BladeName string  `json:"bladename"`
	StartTime string  `json:"starttime"`
	StopTime  string  `json:"stoptime"`
	RSS       float64 `json:"rss"`
	Progress  float64 `json:"progress"`
}

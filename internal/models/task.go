package models

// Task represents a single task within a Tractor job, as stored in the
// engine's "task" table.
type Task struct {
	TID        int     `json:"tid"`
	JID        int     `json:"jid"`
	Title      string  `json:"title"`
	State      string  `json:"state"` // blocked/ready/active/error/done
	Service    string  `json:"service"`
	Progress   float64 `json:"progress"`
	ActiveTime string  `json:"activetime"`
	ReadyTime  string  `json:"readytime"`
	RetryCount int     `json:"retrycount"`
	HasLog     bool    `json:"haslog"`
}

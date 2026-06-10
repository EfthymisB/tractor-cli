// Package models contains data structures returned by the Tractor API.
package models

// Job represents a Tractor render job.
type Job struct {
	JID       int     `json:"jid"`
	Title     string  `json:"title"`
	Owner     string  `json:"owner"`
	Status    string  `json:"status"`
	Priority  float64 `json:"priority"`
	SpoolTime string  `json:"spooltime"`
	StartTime string  `json:"starttime"`
	StopTime  string  `json:"stoptime"`
	NumError  int     `json:"numerror"`
	NumTasks  int     `json:"numtasks"`
	NumDone   int     `json:"numdone"`
	Projects  string  `json:"projects"`  // joined from []string
	SpoolHost string  `json:"spoolhost"`
	Metadata  string  `json:"metadata"`  // formatted for display
}

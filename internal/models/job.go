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
	NumActive int     `json:"numactive"`
	Projects  string  `json:"projects"`  // joined from []string
	SpoolHost string  `json:"spoolhost"`
	Metadata  string  `json:"metadata"`  // raw metadata value

	HasNote   bool   `json:"hasnote"`   // job has at least one note
	Locked    bool   `json:"locked"`    // job is locked
	LockOwner string `json:"lockowner"` // who locked it
	LockNote  string `json:"locknote"`  // the lock note
}

// JobNote is a single note attached to a job.
type JobNote struct {
	Type string  `json:"notetype"`
	User string  `json:"noteuser"`
	Text string  `json:"notetext"`
	Time float64 `json:"notetime"` // epoch seconds
}

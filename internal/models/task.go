package models

// Task represents a single task within a Tractor job, as returned by the
// engine's q=jtree query (the task tree of a job).
type Task struct {
	TID        int     // task id
	JID        int     // owning job id
	Title      string  // task title
	State      string  // decoded: blocked/active/error/done
	Service    string  // service key expression
	Progress   float64 // 0–100, decoded from the state field
	HasLog     bool    // whether the task has a log
	Blade      string  // blade that ran the current invocation, if any
	StateTime  float64 // epoch seconds of last state change
	ActiveTime float64 // epoch seconds the task went active (0 if never)
}

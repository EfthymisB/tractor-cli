package models

// Blade represents a render farm worker node.
type Blade struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Nimby      string `json:"nimby"`
	NumSlots   int    `json:"numslots"`
	AvailSlots int    `json:"availslots"`
	Profile    string `json:"profile"`
	OSLabel    string `json:"oslabel"`
}

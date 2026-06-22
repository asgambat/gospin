package repository

import (
	"encoding/json"
	"reflect"
)

// Metadata holds versioning info for optimistic locking.
type Metadata struct {
	LastUpdate int64 `json:"lastUpdate"` // Unix timestamp in milliseconds
}

// DataDocument represents the persisted JSON structure.
type DataDocument struct {
	Containers []Container `json:"containers" validate:"dive"`
	Order      []string    `json:"order"`
	Groups     []Group     `json:"groups" validate:"dive"`
	GroupOrder []string    `json:"groupOrder"`
	Schedules  []Schedule  `json:"schedules" validate:"dive"`
	Metadata   Metadata    `json:"metadata"`
}

// Container models a single container entry.
type Container struct {
	Running      *bool  `json:"running"`
	Active       *bool  `json:"active" validate:"required"`
	ActivatedAt  *int64 `json:"activatedAt"`
	Favorite     *bool  `json:"favorite"`
	Name         string `json:"name" validate:"required"`
	FriendlyName string `json:"friendly_name" validate:"required"`
	URL          string `json:"url" validate:"required,url"`
}

// Group groups containers by name.
type Group struct {
	Active    *bool    `json:"active" validate:"required"`
	Name      string   `json:"name" validate:"required"`
	Container []string `json:"container"`
}

// Schedule defines timers for a container or group.
type Schedule struct {
	Target     string  `json:"target" validate:"required"`
	TargetType string  `json:"targetType" validate:"required,oneof=container group"`
	ID         string  `json:"id" validate:"required"`
	Timers     []Timer `json:"timers"`
}

// Timer represents a scheduled start/stop window.
type Timer struct {
	Active    *bool  `json:"active" validate:"required"`
	StartTime string `json:"startTime" validate:"required"`
	StopTime  string `json:"stopTime" validate:"required"`
	Days      []int  `json:"days" validate:"dive,min=0,max=6"`
}

// ApplyDefaults sets fallback values after decode.
func (d *DataDocument) ApplyDefaults() {
	for ci := range d.Containers {
		d.Containers[ci].applyDefaults()
	}
	for gi := range d.Groups {
		d.Groups[gi].applyDefaults()
	}
	for si := range d.Schedules {
		d.Schedules[si].applyDefaults()
		for ti := range d.Schedules[si].Timers {
			d.Schedules[si].Timers[ti].applyDefaults()
		}
	}
}

func (t *Group) applyDefaults() {
	if t.Container == nil {
		t.Container = []string{}
	}
	if t.Active == nil {
		v := false
		t.Active = &v
	}
}

func (t *Schedule) applyDefaults() {
	if t.Timers == nil {
		t.Timers = []Timer{}
	}
}

func (t *Container) applyDefaults() {
	if t.Running == nil {
		v := false
		t.Running = &v
	}
	if t.Active == nil {
		v := false
		t.Active = &v
	}
	if t.Favorite == nil {
		v := false
		t.Favorite = &v
	}
}

func (t *Timer) applyDefaults() {
	if t.Active == nil {
		v := false
		t.Active = &v
	}
	if t.Days == nil {
		t.Days = []int{}
	}
}

// AreDataDocumentsEqual compares two DataDocuments ignoring Metadata.
// Uses JSON serialization for flexible comparison (order-independent for object keys).
func AreDataDocumentsEqual(a, b *DataDocument) bool {
	if a == nil || b == nil {
		return a == b
	}

	// Marshal both to JSON
	aBytes, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bBytes, err := json.Marshal(b)
	if err != nil {
		return false
	}

	// Unmarshal to generic maps
	var aMap, bMap map[string]interface{}
	if err := json.Unmarshal(aBytes, &aMap); err != nil {
		return false
	}
	if err := json.Unmarshal(bBytes, &bMap); err != nil {
		return false
	}

	// Remove metadata from comparison
	delete(aMap, "metadata")
	delete(bMap, "metadata")

	return reflect.DeepEqual(aMap, bMap)
}

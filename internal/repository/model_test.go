package repository

import (
	"testing"
)

func boolPtr(b bool) *bool {
	return &b
}

func TestContainer_ApplyDefaults(t *testing.T) {
	c := Container{Name: "test", FriendlyName: "Test", URL: "http://test.local"}
	c.applyDefaults()

	if c.Running == nil {
		t.Error("expected Running to be set")
	}
	if *c.Running != false {
		t.Error("expected Running to default to false")
	}

	if c.Active == nil {
		t.Error("expected Active to be set")
	}
	if *c.Active != false {
		t.Error("expected Active to default to false")
	}
}

func TestContainer_ApplyDefaults_AlreadySet(t *testing.T) {
	c := Container{
		Name:         "test",
		FriendlyName: "Test",
		URL:          "http://test.local",
		Running:      boolPtr(true),
		Active:       boolPtr(true),
	}
	c.applyDefaults()

	if !*c.Running {
		t.Error("expected Running to remain true")
	}
	if !*c.Active {
		t.Error("expected Active to remain true")
	}
}

func TestGroup_ApplyDefaults(t *testing.T) {
	g := Group{Name: "test"}
	g.applyDefaults()

	if g.Container == nil {
		t.Error("expected Container to be initialized")
	}
	if len(g.Container) != 0 {
		t.Error("expected Container to be empty slice")
	}

	if g.Active == nil {
		t.Error("expected Active to be set")
	}
	if *g.Active != false {
		t.Error("expected Active to default to false")
	}
}

func TestSchedule_ApplyDefaults(t *testing.T) {
	s := Schedule{ID: "test", Target: "target", TargetType: "container"}
	s.applyDefaults()

	if s.Timers == nil {
		t.Error("expected Timers to be initialized")
	}
	if len(s.Timers) != 0 {
		t.Error("expected Timers to be empty slice")
	}
}

func TestTimer_ApplyDefaults(t *testing.T) {
	timer := Timer{StartTime: "08:00", StopTime: "18:00"}
	timer.applyDefaults()

	if timer.Active == nil {
		t.Error("expected Active to be set")
	}
	if *timer.Active != false {
		t.Error("expected Active to default to false")
	}

	if timer.Days == nil {
		t.Error("expected Days to be initialized")
	}
	if len(timer.Days) != 0 {
		t.Error("expected Days to be empty slice")
	}
}

func TestDataDocument_ApplyDefaults(t *testing.T) {
	doc := DataDocument{
		Containers: []Container{{Name: "c1", FriendlyName: "C1", URL: "http://c1.local"}},
		Groups:     []Group{{Name: "g1"}},
		Schedules:  []Schedule{{ID: "s1", Target: "c1", TargetType: "container", Timers: []Timer{{StartTime: "08:00", StopTime: "18:00"}}}},
	}

	doc.ApplyDefaults()

	if doc.Containers[0].Running == nil || doc.Containers[0].Active == nil {
		t.Error("expected container defaults to be applied")
	}

	if doc.Groups[0].Active == nil {
		t.Error("expected group defaults to be applied")
	}

	if doc.Schedules[0].Timers[0].Active == nil {
		t.Error("expected timer defaults to be applied")
	}
}

func TestAreDataDocumentsEqual_BothNil(t *testing.T) {
	if !AreDataDocumentsEqual(nil, nil) {
		t.Error("expected nil == nil to be true")
	}
}

func TestAreDataDocumentsEqual_OneNil(t *testing.T) {
	doc := &DataDocument{}
	if AreDataDocumentsEqual(doc, nil) {
		t.Error("expected doc != nil to be false")
	}
	if AreDataDocumentsEqual(nil, doc) {
		t.Error("expected nil != doc to be false")
	}
}

func TestAreDataDocumentsEqual_SameContent(t *testing.T) {
	doc1 := &DataDocument{
		Metadata:   Metadata{LastUpdate: 1000},
		Containers: []Container{{Name: "c1", FriendlyName: "C1", URL: "http://c1.local", Running: boolPtr(false), Active: boolPtr(true)}},
		Order:      []string{"c1"},
	}
	doc2 := &DataDocument{
		Metadata:   Metadata{LastUpdate: 2000}, // Different metadata
		Containers: []Container{{Name: "c1", FriendlyName: "C1", URL: "http://c1.local", Running: boolPtr(false), Active: boolPtr(true)}},
		Order:      []string{"c1"},
	}

	if !AreDataDocumentsEqual(doc1, doc2) {
		t.Error("expected documents with same content (ignoring metadata) to be equal")
	}
}

func TestAreDataDocumentsEqual_DifferentContent(t *testing.T) {
	doc1 := &DataDocument{
		Containers: []Container{{Name: "c1", FriendlyName: "C1", URL: "http://c1.local", Running: boolPtr(false), Active: boolPtr(true)}},
	}
	doc2 := &DataDocument{
		Containers: []Container{{Name: "c2", FriendlyName: "C2", URL: "http://c2.local", Running: boolPtr(false), Active: boolPtr(true)}},
	}

	if AreDataDocumentsEqual(doc1, doc2) {
		t.Error("expected documents with different content to not be equal")
	}
}

func TestAreDataDocumentsEqual_EmptyDocuments(t *testing.T) {
	doc1 := &DataDocument{}
	doc2 := &DataDocument{}

	if !AreDataDocumentsEqual(doc1, doc2) {
		t.Error("expected empty documents to be equal")
	}
}

func TestAreDataDocumentsEqual_DifferentGroups(t *testing.T) {
	doc1 := &DataDocument{
		Groups: []Group{{Name: "g1", Container: []string{"c1"}, Active: boolPtr(true)}},
	}
	doc2 := &DataDocument{
		Groups: []Group{{Name: "g2", Container: []string{"c2"}, Active: boolPtr(false)}},
	}

	if AreDataDocumentsEqual(doc1, doc2) {
		t.Error("expected documents with different groups to not be equal")
	}
}

func TestAreDataDocumentsEqual_DifferentSchedules(t *testing.T) {
	doc1 := &DataDocument{
		Schedules: []Schedule{{ID: "s1", Target: "c1", TargetType: "container"}},
	}
	doc2 := &DataDocument{
		Schedules: []Schedule{{ID: "s2", Target: "c2", TargetType: "group"}},
	}

	if AreDataDocumentsEqual(doc1, doc2) {
		t.Error("expected documents with different schedules to not be equal")
	}
}

func TestAreDataDocumentsEqual_DifferentOrder(t *testing.T) {
	doc1 := &DataDocument{
		Order: []string{"c1", "c2"},
	}
	doc2 := &DataDocument{
		Order: []string{"c2", "c1"},
	}

	if AreDataDocumentsEqual(doc1, doc2) {
		t.Error("expected documents with different order to not be equal")
	}
}

func TestAreDataDocumentsEqual_SameTimers(t *testing.T) {
	doc1 := &DataDocument{
		Schedules: []Schedule{
			{
				ID:         "s1",
				Target:     "c1",
				TargetType: "container",
				Timers: []Timer{
					{StartTime: "08:00", StopTime: "18:00", Days: []int{1, 2, 3}, Active: boolPtr(true)},
				},
			},
		},
	}
	doc2 := &DataDocument{
		Metadata: Metadata{LastUpdate: 9999}, // Different metadata should be ignored
		Schedules: []Schedule{
			{
				ID:         "s1",
				Target:     "c1",
				TargetType: "container",
				Timers: []Timer{
					{StartTime: "08:00", StopTime: "18:00", Days: []int{1, 2, 3}, Active: boolPtr(true)},
				},
			},
		},
	}

	if !AreDataDocumentsEqual(doc1, doc2) {
		t.Error("expected documents with same timers (ignoring metadata) to be equal")
	}
}

// TestAreDataDocumentsEqual_NilDocuments verifies behavior with various nil scenarios.
// This ensures the nil check (a == nil || b == nil) works correctly for all cases.
func TestAreDataDocumentsEqual_NilDocuments(t *testing.T) {
	t.Run("both nil", func(t *testing.T) {
		if !AreDataDocumentsEqual(nil, nil) {
			t.Error("expected both nil documents to be equal")
		}
	})

	t.Run("first nil, second not nil", func(t *testing.T) {
		doc := &DataDocument{
			Containers: []Container{{Name: "c1", FriendlyName: "C1", URL: "http://c1.local", Active: boolPtr(true)}},
		}
		if AreDataDocumentsEqual(nil, doc) {
			t.Error("expected nil != non-nil document to be false")
		}
	})

	t.Run("first not nil, second nil", func(t *testing.T) {
		doc := &DataDocument{
			Containers: []Container{{Name: "c1", FriendlyName: "C1", URL: "http://c1.local", Active: boolPtr(true)}},
		}
		if AreDataDocumentsEqual(doc, nil) {
			t.Error("expected non-nil != nil document to be false")
		}
	})

	t.Run("both non-nil empty", func(t *testing.T) {
		doc1 := &DataDocument{}
		doc2 := &DataDocument{}
		if !AreDataDocumentsEqual(doc1, doc2) {
			t.Error("expected both non-nil empty documents to be equal")
		}
	})
}

// TestAreDataDocumentsEqual_DifferentContentIgnoringMetadata verifies that documents
// are considered equal when they differ only in Metadata.LastUpdate.
// This is a critical test for the watcher callback which should ignore metadata changes.
func TestAreDataDocumentsEqual_DifferentContentIgnoringMetadata(t *testing.T) {
	t.Run("only LastUpdate differs", func(t *testing.T) {
		doc1 := &DataDocument{
			Metadata: Metadata{LastUpdate: 1000},
			Containers: []Container{
				{Name: "c1", FriendlyName: "C1", URL: "http://c1.local", Running: boolPtr(false), Active: boolPtr(true)},
				{Name: "c2", FriendlyName: "C2", URL: "http://c2.local", Running: boolPtr(true), Active: boolPtr(false)},
			},
			Order: []string{"c1", "c2"},
			Groups: []Group{
				{Name: "g1", Container: []string{"c1", "c2"}, Active: boolPtr(true)},
			},
			GroupOrder: []string{"g1"},
		}
		doc2 := &DataDocument{
			Metadata: Metadata{LastUpdate: 5000}, // Completely different timestamp
			Containers: []Container{
				{Name: "c1", FriendlyName: "C1", URL: "http://c1.local", Running: boolPtr(false), Active: boolPtr(true)},
				{Name: "c2", FriendlyName: "C2", URL: "http://c2.local", Running: boolPtr(true), Active: boolPtr(false)},
			},
			Order: []string{"c1", "c2"},
			Groups: []Group{
				{Name: "g1", Container: []string{"c1", "c2"}, Active: boolPtr(true)},
			},
			GroupOrder: []string{"g1"},
		}

		if !AreDataDocumentsEqual(doc1, doc2) {
			t.Error("expected documents with identical content but different LastUpdate to be equal")
		}
	})

	t.Run("metadata only differences with complex structures", func(t *testing.T) {
		doc1 := &DataDocument{
			Metadata: Metadata{LastUpdate: 100},
			Containers: []Container{
				{Name: "app1", FriendlyName: "Application 1", URL: "http://app1:8080", Running: boolPtr(true), Active: boolPtr(true), Favorite: boolPtr(true)},
			},
			Schedules: []Schedule{
				{
					ID:         "sched1",
					Target:     "app1",
					TargetType: "container",
					Timers: []Timer{
						{StartTime: "09:00", StopTime: "17:00", Days: []int{1, 2, 3, 4, 5}, Active: boolPtr(true)},
						{StartTime: "10:00", StopTime: "18:00", Days: []int{0, 6}, Active: boolPtr(false)},
					},
				},
			},
		}
		doc2 := &DataDocument{
			Metadata: Metadata{LastUpdate: 999}, // Very different timestamp
			Containers: []Container{
				{Name: "app1", FriendlyName: "Application 1", URL: "http://app1:8080", Running: boolPtr(true), Active: boolPtr(true), Favorite: boolPtr(true)},
			},
			Schedules: []Schedule{
				{
					ID:         "sched1",
					Target:     "app1",
					TargetType: "container",
					Timers: []Timer{
						{StartTime: "09:00", StopTime: "17:00", Days: []int{1, 2, 3, 4, 5}, Active: boolPtr(true)},
						{StartTime: "10:00", StopTime: "18:00", Days: []int{0, 6}, Active: boolPtr(false)},
					},
				},
			},
		}

		if !AreDataDocumentsEqual(doc1, doc2) {
			t.Error("expected complex documents with identical content but different metadata to be equal")
		}
	})
}

// TestAreDataDocumentsEqual_DeepDifferences verifies that the function correctly
// detects deep differences in Containers, Groups, and Schedules structures.
func TestAreDataDocumentsEqual_DeepDifferences(t *testing.T) {
	t.Run("different container count", func(t *testing.T) {
		doc1 := &DataDocument{
			Containers: []Container{
				{Name: "c1", FriendlyName: "C1", URL: "http://c1.local", Active: boolPtr(true)},
				{Name: "c2", FriendlyName: "C2", URL: "http://c2.local", Active: boolPtr(true)},
			},
		}
		doc2 := &DataDocument{
			Containers: []Container{
				{Name: "c1", FriendlyName: "C1", URL: "http://c1.local", Active: boolPtr(true)},
			},
		}

		if AreDataDocumentsEqual(doc1, doc2) {
			t.Error("expected documents with different container counts to not be equal")
		}
	})

	t.Run("different container properties", func(t *testing.T) {
		doc1 := &DataDocument{
			Containers: []Container{
				{Name: "c1", FriendlyName: "C1", URL: "http://c1.local", Running: boolPtr(true), Active: boolPtr(true)},
			},
		}
		doc2 := &DataDocument{
			Containers: []Container{
				{Name: "c1", FriendlyName: "C1", URL: "http://c1.local", Running: boolPtr(false), Active: boolPtr(true)},
			},
		}

		if AreDataDocumentsEqual(doc1, doc2) {
			t.Error("expected documents with different container Running property to not be equal")
		}
	})

	t.Run("different group count", func(t *testing.T) {
		doc1 := &DataDocument{
			Groups: []Group{
				{Name: "g1", Container: []string{"c1"}, Active: boolPtr(true)},
				{Name: "g2", Container: []string{"c2"}, Active: boolPtr(false)},
			},
		}
		doc2 := &DataDocument{
			Groups: []Group{
				{Name: "g1", Container: []string{"c1"}, Active: boolPtr(true)},
			},
		}

		if AreDataDocumentsEqual(doc1, doc2) {
			t.Error("expected documents with different group counts to not be equal")
		}
	})

	t.Run("different group container membership", func(t *testing.T) {
		doc1 := &DataDocument{
			Groups: []Group{
				{Name: "g1", Container: []string{"c1", "c2"}, Active: boolPtr(true)},
			},
		}
		doc2 := &DataDocument{
			Groups: []Group{
				{Name: "g1", Container: []string{"c1", "c3"}, Active: boolPtr(true)},
			},
		}

		if AreDataDocumentsEqual(doc1, doc2) {
			t.Error("expected documents with different group container membership to not be equal")
		}
	})

	t.Run("different schedule count", func(t *testing.T) {
		doc1 := &DataDocument{
			Schedules: []Schedule{
				{ID: "s1", Target: "c1", TargetType: "container", Timers: []Timer{}},
				{ID: "s2", Target: "c2", TargetType: "container", Timers: []Timer{}},
			},
		}
		doc2 := &DataDocument{
			Schedules: []Schedule{
				{ID: "s1", Target: "c1", TargetType: "container", Timers: []Timer{}},
			},
		}

		if AreDataDocumentsEqual(doc1, doc2) {
			t.Error("expected documents with different schedule counts to not be equal")
		}
	})

	t.Run("different schedule timer properties", func(t *testing.T) {
		doc1 := &DataDocument{
			Schedules: []Schedule{
				{
					ID:         "s1",
					Target:     "c1",
					TargetType: "container",
					Timers: []Timer{
						{StartTime: "08:00", StopTime: "18:00", Days: []int{1, 2, 3}, Active: boolPtr(true)},
					},
				},
			},
		}
		doc2 := &DataDocument{
			Schedules: []Schedule{
				{
					ID:         "s1",
					Target:     "c1",
					TargetType: "container",
					Timers: []Timer{
						{StartTime: "09:00", StopTime: "18:00", Days: []int{1, 2, 3}, Active: boolPtr(true)},
					},
				},
			},
		}

		if AreDataDocumentsEqual(doc1, doc2) {
			t.Error("expected documents with different timer start times to not be equal")
		}
	})

	t.Run("different schedule timer days", func(t *testing.T) {
		doc1 := &DataDocument{
			Schedules: []Schedule{
				{
					ID:         "s1",
					Target:     "c1",
					TargetType: "container",
					Timers: []Timer{
						{StartTime: "08:00", StopTime: "18:00", Days: []int{1, 2, 3}, Active: boolPtr(true)},
					},
				},
			},
		}
		doc2 := &DataDocument{
			Schedules: []Schedule{
				{
					ID:         "s1",
					Target:     "c1",
					TargetType: "container",
					Timers: []Timer{
						{StartTime: "08:00", StopTime: "18:00", Days: []int{1, 2, 3, 4}, Active: boolPtr(true)},
					},
				},
			},
		}

		if AreDataDocumentsEqual(doc1, doc2) {
			t.Error("expected documents with different timer days to not be equal")
		}
	})

	t.Run("complex scenario - multiple differences", func(t *testing.T) {
		doc1 := &DataDocument{
			Metadata: Metadata{LastUpdate: 1000},
			Containers: []Container{
				{Name: "c1", FriendlyName: "C1", URL: "http://c1.local", Running: boolPtr(true), Active: boolPtr(true), Favorite: boolPtr(false)},
			},
			Groups: []Group{
				{Name: "g1", Container: []string{"c1"}, Active: boolPtr(true)},
			},
			Schedules: []Schedule{
				{
					ID:         "s1",
					Target:     "g1",
					TargetType: "group",
					Timers: []Timer{
						{StartTime: "08:00", StopTime: "17:00", Days: []int{1, 2, 3, 4, 5}, Active: boolPtr(true)},
					},
				},
			},
		}
		doc2 := &DataDocument{
			Metadata: Metadata{LastUpdate: 2000}, // Different metadata (should be ignored)
			Containers: []Container{
				{Name: "c1", FriendlyName: "C1", URL: "http://c1.local", Running: boolPtr(false), Active: boolPtr(true), Favorite: boolPtr(false)}, // Running is different
			},
			Groups: []Group{
				{Name: "g1", Container: []string{"c1"}, Active: boolPtr(true)},
			},
			Schedules: []Schedule{
				{
					ID:         "s1",
					Target:     "g1",
					TargetType: "group",
					Timers: []Timer{
						{StartTime: "08:00", StopTime: "17:00", Days: []int{1, 2, 3, 4, 5}, Active: boolPtr(true)},
					},
				},
			},
		}

		if AreDataDocumentsEqual(doc1, doc2) {
			t.Error("expected documents with different Running property to not be equal (metadata difference should be ignored)")
		}
	})
}

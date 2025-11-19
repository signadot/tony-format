package codegen

import (
	"reflect"
	"testing"
)

func TestTopologicalSort(t *testing.T) {
	// Create some dummy structs
	structA := &StructInfo{Name: "A", Fields: []*FieldInfo{}}
	structB := &StructInfo{Name: "B", Fields: []*FieldInfo{
		{Name: "FieldA", StructTypeName: "A", Type: reflect.TypeOf(struct{}{})},
	}}
	structC := &StructInfo{Name: "C", Fields: []*FieldInfo{
		{Name: "FieldB", StructTypeName: "B", Type: reflect.TypeOf(struct{}{})},
	}}
	structD := &StructInfo{Name: "D", Fields: []*FieldInfo{}} // Independent

	tests := []struct {
		name    string
		structs []*StructInfo
		want    []string // Names in expected order (partial check)
		wantErr bool
	}{
		{
			name:    "Simple chain A -> B -> C",
			structs: []*StructInfo{structC, structA, structB},
			want:    []string{"A", "B", "C"},
		},
		{
			name:    "Independent D",
			structs: []*StructInfo{structA, structD},
			want:    []string{"A", "D"}, // Or D, A. Sort is deterministic so A, D (alphabetical for independent)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			graph, err := BuildDependencyGraph(tt.structs)
			if err != nil {
				t.Fatalf("BuildDependencyGraph() error = %v", err)
			}

			got, err := TopologicalSort(graph)
			if (err != nil) != tt.wantErr {
				t.Errorf("TopologicalSort() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				var gotNames []string
				for _, s := range got {
					gotNames = append(gotNames, s.Name)
				}

				// Check order constraints
				// For A->B->C, index(A) < index(B) < index(C)
				indices := make(map[string]int)
				for i, name := range gotNames {
					indices[name] = i
				}

				if len(tt.want) == 3 && tt.want[0] == "A" {
					if indices["A"] > indices["B"] || indices["B"] > indices["C"] {
						t.Errorf("TopologicalSort() order = %v, want A before B before C", gotNames)
					}
				}
			}
		})
	}
}

func TestDetectCycles(t *testing.T) {
	// Cycle A -> B -> A
	structA := &StructInfo{Name: "A", Fields: []*FieldInfo{
		{Name: "FieldB", StructTypeName: "B"},
	}}
	structB := &StructInfo{Name: "B", Fields: []*FieldInfo{
		{Name: "FieldA", StructTypeName: "A"},
	}}

	structs := []*StructInfo{structA, structB}
	graph, err := BuildDependencyGraph(structs)
	if err != nil {
		t.Fatalf("BuildDependencyGraph() error = %v", err)
	}

	cycles, err := DetectCycles(graph)
	if err != nil {
		t.Fatalf("DetectCycles() error = %v", err)
	}

	if len(cycles) == 0 {
		t.Error("DetectCycles() expected cycle, got none")
	}
}

package gomap

import (
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
)

// CustomTime implements TextMarshaler and TextUnmarshaler
type CustomTime struct {
	time.Time
}

func (ct CustomTime) MarshalText() ([]byte, error) {
	return []byte(ct.Time.Format(time.RFC3339)), nil
}

func (ct *CustomTime) UnmarshalText(text []byte) error {
	t, err := time.Parse(time.RFC3339, string(text))
	if err != nil {
		return err
	}
	ct.Time = t
	return nil
}

type StructWithTextMarshaler struct {
	Name      string     `tony:"field=name"`
	CreatedAt CustomTime `tony:"field=created_at"`
}

func TestMarshalText(t *testing.T) {
	now := time.Date(2023, 10, 26, 12, 0, 0, 0, time.UTC)
	s := StructWithTextMarshaler{
		Name:      "test",
		CreatedAt: CustomTime{Time: now},
	}

	// Test ToTonyIR
	node, err := ToTonyIR(s)
	if err != nil {
		t.Fatalf("ToTonyIR() error = %v", err)
	}
	if node == nil {
		t.Fatal("ToTonyIR() returned nil node")
	}

	// Check if CreatedAt is marshaled as string
	t.Logf("Node: %v", node)
	if node.Type == ir.ObjectType {
		t.Logf("Num Fields: %d", len(node.Fields))
		for i, f := range node.Fields {
			t.Logf("Field %d: %s", i, f.String)
		}
	}
	createdAtNode := ir.Get(node, "created_at")
	if createdAtNode == nil {
		t.Fatal("createdAtNode is nil")
	}

	if createdAtNode.Type != ir.StringType {
		t.Errorf("Expected StringType for TextMarshaler, got %v", createdAtNode.Type)
	}
	expectedStr := now.Format(time.RFC3339)
	if createdAtNode.String != expectedStr {
		t.Errorf("Expected %q, got %q", expectedStr, createdAtNode.String)
	}
}

func TestUnmarshalText(t *testing.T) {
	now := time.Date(2023, 10, 26, 12, 0, 0, 0, time.UTC)
	nowStr := now.Format(time.RFC3339)

	node := ir.FromMap(map[string]*ir.Node{
		"name":       ir.FromString("test"),
		"created_at": ir.FromString(nowStr),
	})

	var s StructWithTextMarshaler
	// Test FromTonyIR
	err := FromTonyIR(node, &s)
	if err != nil {
		t.Fatalf("FromTonyIR() error = %v", err)
	}

	if s.Name != "test" {
		t.Errorf("Expected Name = %q, got %q", "test", s.Name)
	}
	if !s.CreatedAt.Time.Equal(now) {
		t.Errorf("Expected CreatedAt = %v, got %v", now, s.CreatedAt.Time)
	}
}

package gomap

import (
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, err)
	require.NotNil(t, node)

	// Check if CreatedAt is marshaled as string
	t.Logf("Node: %v", node)
	if node.Type == ir.ObjectType {
		t.Logf("Num Fields: %d", len(node.Fields))
		for i, f := range node.Fields {
			t.Logf("Field %d: %s", i, f.String)
		}
	}
	createdAtNode := ir.Get(node, "created_at")
	require.NotNil(t, createdAtNode)

	assert.Equal(t, ir.StringType, createdAtNode.Type, "Expected StringType for TextMarshaler")
	assert.Equal(t, now.Format(time.RFC3339), createdAtNode.String)
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
	require.NoError(t, err)

	assert.Equal(t, "test", s.Name)
	assert.Equal(t, now, s.CreatedAt.Time)
}

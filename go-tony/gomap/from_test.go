package gomap

import (
	"reflect"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

func TestFromTonyIR_BasicTypes(t *testing.T) {
	tests := []struct {
		name    string
		node    *ir.Node
		want    interface{}
		wantErr bool
	}{
		{
			name:    "string",
			node:    ir.FromString("hello"),
			want:    "hello",
			wantErr: false,
		},
		{
			name:    "int",
			node:    ir.FromInt(42),
			want:    42,
			wantErr: false,
		},
		{
			name:    "int64",
			node:    ir.FromInt(123456789),
			want:    int64(123456789),
			wantErr: false,
		},
		{
			name:    "float64",
			node:    ir.FromFloat(3.14),
			want:    3.14,
			wantErr: false,
		},
		{
			name:    "bool true",
			node:    ir.FromBool(true),
			want:    true,
			wantErr: false,
		},
		{
			name:    "bool false",
			node:    ir.FromBool(false),
			want:    false,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a pointer to the expected type
			val := reflect.New(reflect.TypeOf(tt.want))
			err := FromTonyIR(tt.node, val.Interface())
			if (err != nil) != tt.wantErr {
				t.Errorf("FromTonyIR() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				got := val.Elem().Interface()
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("FromTonyIR() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestFromTonyIR_Nil(t *testing.T) {
	var str string
	err := FromTonyIR(ir.Null(), &str)
	if err != nil {
		t.Fatalf("FromTonyIR(nil) error = %v", err)
	}
	if str != "" {
		t.Errorf("FromTonyIR(nil) = %q, want empty string", str)
	}
}

func TestFromTonyIR_Pointers(t *testing.T) {
	t.Run("string pointer", func(t *testing.T) {
		node := ir.FromString("hello")
		var strPtr *string
		err := FromTonyIR(node, &strPtr)
		if err != nil {
			t.Fatalf("FromTonyIR() error = %v", err)
		}
		if strPtr == nil || *strPtr != "hello" {
			t.Errorf("FromTonyIR() = %v, want pointer to 'hello'", strPtr)
		}
	})

	t.Run("int pointer", func(t *testing.T) {
		node := ir.FromInt(42)
		var intPtr *int
		err := FromTonyIR(node, &intPtr)
		if err != nil {
			t.Fatalf("FromTonyIR() error = %v", err)
		}
		if intPtr == nil || *intPtr != 42 {
			t.Errorf("FromTonyIR() = %v, want pointer to 42", intPtr)
		}
	})

	t.Run("nil pointer", func(t *testing.T) {
		node := ir.Null()
		var strPtr *string
		err := FromTonyIR(node, &strPtr)
		if err != nil {
			t.Fatalf("FromTonyIR() error = %v", err)
		}
		if strPtr != nil {
			t.Errorf("FromTonyIR() = %v, want nil", strPtr)
		}
	})
}

func stringPtrUnmarshal(s string) *string {
	return &s
}

func intPtrUnmarshal(i int) *int {
	return &i
}

func TestFromTonyIR_Slices(t *testing.T) {
	tests := []struct {
		name    string
		node    *ir.Node
		want    interface{}
		wantErr bool
	}{
		{
			name:    "string slice",
			node:    ir.FromSlice([]*ir.Node{ir.FromString("a"), ir.FromString("b"), ir.FromString("c")}),
			want:    []string{"a", "b", "c"},
			wantErr: false,
		},
		{
			name:    "int slice",
			node:    ir.FromSlice([]*ir.Node{ir.FromInt(1), ir.FromInt(2), ir.FromInt(3)}),
			want:    []int{1, 2, 3},
			wantErr: false,
		},
		{
			name:    "empty slice",
			node:    ir.FromSlice([]*ir.Node{}),
			want:    []string{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := reflect.New(reflect.TypeOf(tt.want))
			err := FromTonyIR(tt.node, val.Interface())
			if (err != nil) != tt.wantErr {
				t.Errorf("FromTonyIR() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				got := val.Elem().Interface()
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("FromTonyIR() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestFromTonyIR_Arrays(t *testing.T) {
	node := ir.FromSlice([]*ir.Node{ir.FromInt(1), ir.FromInt(2), ir.FromInt(3)})
	var arr [3]int
	err := FromTonyIR(node, &arr)
	if err != nil {
		t.Fatalf("FromTonyIR() error = %v", err)
	}
	want := [3]int{1, 2, 3}
	if arr != want {
		t.Errorf("FromTonyIR() = %v, want %v", arr, want)
	}
}

func TestFromTonyIR_ArrayLengthMismatch(t *testing.T) {
	node := ir.FromSlice([]*ir.Node{ir.FromInt(1), ir.FromInt(2)})
	var arr [3]int
	err := FromTonyIR(node, &arr)
	if err == nil {
		t.Error("FromTonyIR() expected error for length mismatch, got nil")
	}
}

func TestFromTonyIR_Maps(t *testing.T) {
	tests := []struct {
		name    string
		node    *ir.Node
		want    interface{}
		wantErr bool
	}{
		{
			name:    "string map",
			node:    ir.FromMap(map[string]*ir.Node{"a": ir.FromString("1"), "b": ir.FromString("2")}),
			want:    map[string]string{"a": "1", "b": "2"},
			wantErr: false,
		},
		{
			name:    "int map",
			node:    ir.FromMap(map[string]*ir.Node{"x": ir.FromInt(10), "y": ir.FromInt(20)}),
			want:    map[string]int{"x": 10, "y": 20},
			wantErr: false,
		},
		{
			name:    "empty map",
			node:    ir.FromMap(map[string]*ir.Node{}),
			want:    map[string]string{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := reflect.New(reflect.TypeOf(tt.want))
			err := FromTonyIR(tt.node, val.Interface())
			if (err != nil) != tt.wantErr {
				t.Errorf("FromTonyIR() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				got := val.Elem().Interface()
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("FromTonyIR() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestFromTonyIR_Structs(t *testing.T) {
	type Person struct {
		Name string
		Age  int
	}

	node := ir.FromMap(map[string]*ir.Node{
		"Name": ir.FromString("Alice"),
		"Age":  ir.FromInt(30),
	})

	var p Person
	err := FromTonyIR(node, &p)
	if err != nil {
		t.Fatalf("FromTonyIR() error = %v", err)
	}

	if p.Name != "Alice" {
		t.Errorf("Name = %q, want 'Alice'", p.Name)
	}
	if p.Age != 30 {
		t.Errorf("Age = %d, want 30", p.Age)
	}
}

func TestFromTonyIR_StructsWithExtraFields(t *testing.T) {
	type Person struct {
		Name string
		Age  int
	}

	// IR node has extra field "Email" that doesn't exist in struct
	node := ir.FromMap(map[string]*ir.Node{
		"Name":  ir.FromString("Bob"),
		"Age":   ir.FromInt(25),
		"Email": ir.FromString("bob@example.com"),
	})

	var p Person
	err := FromTonyIR(node, &p)
	// Extra fields should be silently ignored
	if err != nil {
		t.Fatalf("FromTonyIR() error = %v", err)
	}

	if p.Name != "Bob" {
		t.Errorf("Name = %q, want 'Bob'", p.Name)
	}
	if p.Age != 25 {
		t.Errorf("Age = %d, want 25", p.Age)
	}
}

func TestFromTonyIR_EmbeddedStructs(t *testing.T) {
	type Address struct {
		Street string
		City   string
	}

	type Person struct {
		Name string
		Address
	}

	// IR node has flattened fields (Street, City at top level)
	// The unmarshaling code should handle embedded structs by checking
	// if fields exist in embedded structs
	node := ir.FromMap(map[string]*ir.Node{
		"Name":   ir.FromString("Charlie"),
		"Street": ir.FromString("123 Main"),
		"City":   ir.FromString("NYC"),
	})

	var p Person
	err := FromTonyIR(node, &p)
	if err != nil {
		t.Fatalf("FromTonyIR() error = %v", err)
	}

	if p.Name != "Charlie" {
		t.Errorf("Name = %q, want 'Charlie'", p.Name)
	}
	if p.Address.Street != "123 Main" {
		t.Errorf("Address.Street = %q, want '123 Main'", p.Address.Street)
	}
	if p.Address.City != "NYC" {
		t.Errorf("Address.City = %q, want 'NYC'", p.Address.City)
	}
}

func TestFromTonyIR_NestedStructs(t *testing.T) {
	type Address struct {
		Street string
		City   string
	}

	type Person struct {
		Name    string
		Address Address // not embedded
	}

	// IR node has nested Address object
	node := ir.FromMap(map[string]*ir.Node{
		"Name": ir.FromString("Dave"),
		"Address": ir.FromMap(map[string]*ir.Node{
			"Street": ir.FromString("456 Oak"),
			"City":   ir.FromString("LA"),
		}),
	})

	var p Person
	err := FromTonyIR(node, &p)
	if err != nil {
		t.Fatalf("FromTonyIR() error = %v", err)
	}

	if p.Name != "Dave" {
		t.Errorf("Name = %q, want 'Dave'", p.Name)
	}
	if p.Address.Street != "456 Oak" {
		t.Errorf("Address.Street = %q, want '456 Oak'", p.Address.Street)
	}
	if p.Address.City != "LA" {
		t.Errorf("Address.City = %q, want 'LA'", p.Address.City)
	}
}

func TestFromTonyIR_ComplexNested(t *testing.T) {
	type Address struct {
		Street string
		City   string
	}

	type Person struct {
		Name    string
		Age     int
		Tags    []string
		Address Address
	}

	node := ir.FromMap(map[string]*ir.Node{
		"Name": ir.FromString("Eve"),
		"Age":  ir.FromInt(28),
		"Tags": ir.FromSlice([]*ir.Node{
			ir.FromString("developer"),
			ir.FromString("golang"),
		}),
		"Address": ir.FromMap(map[string]*ir.Node{
			"Street": ir.FromString("789 Pine"),
			"City":   ir.FromString("SF"),
		}),
	})

	var p Person
	err := FromTonyIR(node, &p)
	if err != nil {
		t.Fatalf("FromTonyIR() error = %v", err)
	}

	if p.Name != "Eve" {
		t.Errorf("Name = %q, want 'Eve'", p.Name)
	}
	if p.Age != 28 {
		t.Errorf("Age = %d, want 28", p.Age)
	}
	if len(p.Tags) != 2 || p.Tags[0] != "developer" || p.Tags[1] != "golang" {
		t.Errorf("Tags = %v, want ['developer', 'golang']", p.Tags)
	}
	if p.Address.Street != "789 Pine" {
		t.Errorf("Address.Street = %q, want '789 Pine'", p.Address.Street)
	}
	if p.Address.City != "SF" {
		t.Errorf("Address.City = %q, want 'SF'", p.Address.City)
	}
}

func TestFromTonyIR_TypeMismatch(t *testing.T) {
	tests := []struct {
		name    string
		node    *ir.Node
		dest    interface{}
		wantErr bool
	}{
		{
			name:    "string to int",
			node:    ir.FromString("hello"),
			dest:    new(int),
			wantErr: true,
		},
		{
			name:    "int to string",
			node:    ir.FromInt(42),
			dest:    new(string),
			wantErr: true,
		},
		{
			name:    "array to string",
			node:    ir.FromSlice([]*ir.Node{ir.FromString("a")}),
			dest:    new(string),
			wantErr: true,
		},
		{
			name:    "object to string",
			node:    ir.FromMap(map[string]*ir.Node{"x": ir.FromString("y")}),
			dest:    new(string),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := FromTonyIR(tt.node, tt.dest)
			if (err != nil) != tt.wantErr {
				t.Errorf("FromTonyIR() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFromTonyIR_IntOverflow(t *testing.T) {
	// Test int8 overflow
	node := ir.FromInt(1000) // Too large for int8
	var val int8
	err := FromTonyIR(node, &val)
	if err == nil {
		t.Error("FromTonyIR() expected error for int8 overflow, got nil")
	}
}

func TestFromTonyIR_UintFromNegative(t *testing.T) {
	// Test uint from negative number
	node := ir.FromInt(-5)
	var val uint
	err := FromTonyIR(node, &val)
	if err == nil {
		t.Error("FromTonyIR() expected error for negative uint, got nil")
	}
}

func TestFromTonyIR_RoundTrip(t *testing.T) {
	type Person struct {
		Name    string
		Age     int
		Active  bool
		Tags    []string
		Details map[string]string
	}

	original := Person{
		Name:    "Frank",
		Age:     40,
		Active:  true,
		Tags:    []string{"manager", "tech"},
		Details: map[string]string{"dept": "eng", "level": "senior"},
	}

	// Round trip: Go -> IR -> Go
	node, err := ToTonyIR(original)
	if err != nil {
		t.Fatalf("ToTonyIR() error = %v", err)
	}

	var result Person
	err = FromTonyIR(node, &result)
	if err != nil {
		t.Fatalf("FromIR() error = %v", err)
	}

	if !reflect.DeepEqual(result, original) {
		t.Errorf("Round trip failed: got %+v, want %+v", result, original)
	}
}

func TestFromTonyIR_InvalidDestination(t *testing.T) {
	node := ir.FromString("hello")

	tests := []struct {
		name    string
		dest    interface{}
		wantErr bool
	}{
		{
			name:    "nil destination",
			dest:    nil,
			wantErr: true,
		},
		{
			name:    "non-pointer destination",
			dest:    "not a pointer",
			wantErr: true,
		},
		{
			name:    "nil pointer destination",
			dest:    (*string)(nil),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := FromTonyIR(node, tt.dest)
			if (err != nil) != tt.wantErr {
				t.Errorf("FromIR() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

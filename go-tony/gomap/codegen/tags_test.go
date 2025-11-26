package codegen

import (
	"reflect"
	"testing"
)

func TestParseStructTag(t *testing.T) {
	tests := []struct {
		name    string
		tag     string
		want    map[string]string
		wantErr bool
	}{
		{
			name: "Empty tag",
			tag:  "",
			want: map[string]string{},
		},
		{
			name: "Single flag",
			tag:  "optional",
			want: map[string]string{"optional": ""},
		},
		{
			name: "Single key-value",
			tag:  "field=name",
			want: map[string]string{"field": "name"},
		},
		{
			name: "Multiple flags",
			tag:  "optional,omit",
			want: map[string]string{"optional": "", "omit": ""},
		},
		{
			name: "Mixed flags and key-value",
			tag:  "field=name,optional",
			want: map[string]string{"field": "name", "optional": ""},
		},
		{
			name: "Quoted value",
			tag:  `field="name with spaces"`,
			want: map[string]string{"field": "name with spaces"},
		},
		{
			name: "Mixed with quoted value",
			tag:  `schemagen=person,desc="A person struct"`,
			want: map[string]string{"schemagen": "person", "desc": "A person struct"},
		},
		{
			name: "Spaces around commas",
			tag:  "field=name , optional", // Parser currently accumulates spaces in keys/values if not trimmed carefully
			// My implementation trims keys.
			want: map[string]string{"field": "name", "optional": ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseStructTag(tt.tag)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseStructTag() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseStructTag() = %v, want %v", got, tt.want)
			}
		})
	}
}

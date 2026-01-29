package dirbuild

import (
	"bytes"
	"os"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
)

func TestDirGoMap(t *testing.T) {
	dir := &Dir{
		Sources: []DirSource{
			{
				Dir: ptr("srcDir"),
			},
		},
		Patches: []DirPatch{
			{
				If:    "zoo",
				Match: ir.Null().WithTag("!pass"),
				Patch: ir.FromSlice([]*ir.Node{
					ir.FromBool(true),
				}),
			},
		},
		DestDir: "destDir",
		Env: map[string]*ir.Node{
			"fred": ir.FromMap(map[string]*ir.Node{
				"barney": ir.FromString("wilma"),
			},
			),
		},
	}
	n, err := gomap.ToTonyIR(dir)
	if err != nil {
		t.Error(err)
		return
	}
	altDir := &Dir{}
	if err := gomap.FromTonyIR(n, altDir); err != nil {
		t.Error(err)
		return
	}
	back, err := gomap.ToTonyIR(altDir)
	if err != nil {
		t.Error(err)
		return
	}
	buf1 := bytes.NewBuffer(nil)
	if err := encode.Encode(n, buf1); err != nil {
		t.Error(err)
		return
	}
	buf2 := bytes.NewBuffer(nil)
	if err := encode.Encode(back, buf2); err != nil {
		t.Error(err)
		return
	}
	if bytes.Equal(buf1.Bytes(), buf2.Bytes()) {
		return
	}
	encode.Encode(n, os.Stdout)
	encode.Encode(back, os.Stdout)
	t.Errorf("mismatch")
}

func TestK8sFilenames(t *testing.T) {
	// Build a k8s-style IR node: {apiVersion: apps/v1, kind: Deployment, metadata: {name: my-app, namespace: prod}}
	k8sNode := ir.FromMap(map[string]*ir.Node{
		"apiVersion": ir.FromString("apps/v1"),
		"kind":       ir.FromString("Deployment"),
		"metadata": ir.FromMap(map[string]*ir.Node{
			"name":      ir.FromString("my-app"),
			"namespace": ir.FromString("prod"),
		}),
	})

	tests := []struct {
		name    string
		pattern string
		want    string
	}{
		{"name-kind", "name-kind", "my-app-deployment"},
		{"kind-name", "kind-name", "deployment-my-app"},
		{"name-kind-namespace", "name-kind-namespace", "my-app-deployment-prod"},
		{"name only", "name", "my-app"},
		{"namespace-kind", "namespace-kind", "prod-deployment"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Dir{
				Output: &DirOutput{
					K8s: &DirOutputK8s{
						Filenames: tt.pattern,
					},
				},
			}
			got := d.fileName(k8sNode)
			if got != tt.want {
				t.Errorf("fileName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestK8sFilenamesDefaults(t *testing.T) {
	// k8s node with no namespace
	node := ir.FromMap(map[string]*ir.Node{
		"apiVersion": ir.FromString("v1"),
		"kind":       ir.FromString("Service"),
		"metadata": ir.FromMap(map[string]*ir.Node{
			"name": ir.FromString("frontend"),
		}),
	})

	d := &Dir{
		Output: &DirOutput{
			K8s: &DirOutputK8s{Filenames: "name-kind-namespace"},
		},
	}
	got := d.fileName(node)
	if got != "frontend-service-default" {
		t.Errorf("fileName() = %q, want %q", got, "frontend-service-default")
	}
}

func TestK8sFilenamesFallbackWithoutKind(t *testing.T) {
	// Non-k8s object (no kind field) should fall back to default logic
	node := ir.FromMap(map[string]*ir.Node{
		"name": ir.FromString("something"),
	})

	d := &Dir{
		Output: &DirOutput{
			K8s: &DirOutputK8s{Filenames: "name-kind"},
		},
	}
	got := d.fileName(node)
	// No kind → falls through to default FileName() which uses the top-level name
	if got != "something" {
		t.Errorf("fileName() = %q, want %q", got, "something")
	}
}

func TestK8sFilenamesNotConfigured(t *testing.T) {
	// When k8s.filenames is not configured, use default logic (name-kind-namespace)
	node := ir.FromMap(map[string]*ir.Node{
		"apiVersion": ir.FromString("v1"),
		"kind":       ir.FromString("ConfigMap"),
		"metadata": ir.FromMap(map[string]*ir.Node{
			"name":      ir.FromString("my-config"),
			"namespace": ir.FromString("kube-system"),
		}),
	})

	d := &Dir{
		Output: &DirOutput{},
	}
	got := d.fileName(node)
	if got != "my-config-configmap-kube-system" {
		t.Errorf("fileName() = %q, want %q", got, "my-config-configmap-kube-system")
	}
}

func TestK8sFilenamesValidation(t *testing.T) {
	o := &DirOutput{
		K8s: &DirOutputK8s{Filenames: "name-bogus"},
	}
	err := o.validate()
	if err == nil {
		t.Fatal("expected validation error for invalid token")
	}

	o2 := &DirOutput{
		K8s: &DirOutputK8s{Filenames: "name-kind"},
	}
	if err := o2.validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestFileNameTag(t *testing.T) {
	// !filename tag should still take priority over k8s pattern
	node := ir.FromMap(map[string]*ir.Node{
		"kind": ir.FromString("Deployment"),
		"metadata": ir.FromMap(map[string]*ir.Node{
			"name": ir.FromString("my-app"),
		}),
	}).WithTag("!filename(custom-name)")

	d := &Dir{
		Output: &DirOutput{
			K8s: &DirOutputK8s{Filenames: "name-kind"},
		},
	}
	got := d.fileName(node)
	if got != "custom-name" {
		t.Errorf("fileName() = %q, want %q", got, "custom-name")
	}
}

func ptr[T any](v T) *T { return &v }

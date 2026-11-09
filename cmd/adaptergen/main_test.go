package main

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateSourceFromInterface(t *testing.T) {
	dir := t.TempDir()
	source := `package example
import "time"
type Operations interface {
    Position() (time.Time, error)
    SetPosition(time.Time) (time.Time, error)
    Finish() error
}`
	if err := os.WriteFile(filepath.Join(dir, "contract.go"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	generated, err := generateSource(dir, "Operations")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "operations_adapter_gen.go", generated, 0); err != nil {
		t.Fatalf("generated source is invalid Go: %v", err)
	}
	for _, want := range []string{
		`"time"`,
		`Operations:position.get`,
		`Operations:position.set`,
		`Operations:finish.call`,
		`Resolve[time.Time]`,
		`Resolve[Command]`,
		`reflect.TypeFor[Operations]()`,
	} {
		if !strings.Contains(string(generated), want) {
			t.Fatalf("generated source lacks %q:\n%s", want, generated)
		}
	}
}

func TestGenerateSourceRejectsUnsupportedContracts(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		want   string
	}{
		{"not interface", "package example\ntype Operations struct{}", "not an interface"},
		{"no error", "package example\ntype Operations interface { Position() int }", "expected error"},
		{"embedded", "package example\ntype Base interface { Finish() error }; type Operations interface { Base }", "embedded"},
		{"variadic", "package example\ntype Operations interface { Update(...int) error }", "variadic"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "contract.go"), []byte(test.source), 0644); err != nil {
				t.Fatal(err)
			}
			_, err := generateSource(dir, "Operations")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("want %q error, got %v", test.want, err)
			}
		})
	}
}

package api

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestEveryMethodIsInTheSchema: each Method constant of the package is a
// method the schema describes. If it regresses, a method added to the
// socket is missing from what plugins and scripts are told it takes.
func TestEveryMethodIsInTheSchema(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, e.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			vs, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for i, name := range vs.Names {
				if !strings.HasPrefix(name.Name, "Method") || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok {
					continue
				}
				method, _ := strconv.Unquote(lit.Value)
				found++
				if _, ok := methodParams[method]; !ok {
					t.Errorf("%s (%q) is not in the schema", name.Name, method)
				}
			}
			return true
		})
	}
	if found != len(methodParams) {
		t.Errorf("%d methods declared, %d in the schema", found, len(methodParams))
	}
}

// TestTheSchemaHoldsTogether: every reference resolves, every required
// field is a field, every request names its method. If it regresses, the
// schema fails a validator before it is used.
func TestTheSchemaHoldsTogether(t *testing.T) {
	data, err := json.Marshal(Schema())
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	request := doc["schemas"].(map[string]any)["request"].(map[string]any)
	defs := request["$defs"].(map[string]any)
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if ref, ok := x["$ref"].(string); ok {
				name := strings.TrimPrefix(ref, "#/schemas/request/$defs/")
				if _, ok := defs[name]; !ok {
					t.Errorf("%s does not resolve", ref)
				}
			}
			if req, ok := x["required"].([]any); ok {
				props, _ := x["properties"].(map[string]any)
				for _, r := range req {
					if _, ok := props[r.(string)]; !ok {
						t.Errorf("required %v is not a property", r)
					}
				}
			}
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(doc)
	methods := map[string]bool{}
	for _, r := range request["oneOf"].([]any) {
		m := r.(map[string]any)["properties"].(map[string]any)["method"].(map[string]any)["const"].(string)
		methods[m] = true
	}
	for _, m := range []string{MethodTabCreate, MethodAgentPrompt, MethodEventsSubscribe, MethodPing} {
		if !methods[m] {
			t.Errorf("%s has no request schema", m)
		}
	}
	tab := defs["TabCreateParams"].(map[string]any)
	if req := tab["required"].([]any); len(req) != 1 || req[0] != "workspace_id" {
		t.Errorf("tab.create requires %v", req)
	}
}

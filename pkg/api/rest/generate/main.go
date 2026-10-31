// Command generate adapts nullable type unions for oapi-codegen without modifying the approved contract.
package main

import (
	"fmt"
	"go.yaml.in/yaml/v3"
	"os"
	"os/exec"
)

func normalize(v any) {
	switch x := v.(type) {
	case map[string]any:
		delete(x, "discriminator")
		if ts, ok := x["type"].([]any); ok {
			for _, t := range ts {
				if t != "null" {
					x["type"] = t
				}
			}
			x["nullable"] = true
		}
		for _, v := range x {
			normalize(v)
		}
	case []any:
		for _, v := range x {
			normalize(v)
		}
	}
}
func main() {
	b, e := os.ReadFile("openapi.yaml")
	if e != nil {
		panic(e)
	}
	var doc map[string]any
	if e = yaml.Unmarshal(b, &doc); e != nil {
		panic(e)
	}
	normalize(doc)
	components := object(doc["components"])
	for name, p := range object(components["parameters"]) {
		object(p)["x-go-name"] = "Parameter_" + name
	}
	doc["openapi"] = "3.0.3"
	b, e = yaml.Marshal(doc)
	if e != nil {
		panic(e)
	}
	f, e := os.CreateTemp("", "kannon-openapi-*.yaml")
	if e != nil {
		panic(e)
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(b); e != nil {
		panic(e)
	}
	f.Close()
	c := exec.Command("go", "run", "github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.6.0", "-generate", "types,std-http-server", "-package", "rest", "-o", "types.gen.go", f.Name())
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if e = c.Run(); e != nil {
		panic(fmt.Sprint(e))
	}
}

func object(v any) map[string]any {
	m, ok := v.(map[string]any)
	if !ok {
		panic("expected OpenAPI object")
	}
	return m
}

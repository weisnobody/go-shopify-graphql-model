package main

import (
	"fmt"
	"go/types"
	"os"
	"strings"

	"github.com/99designs/gqlgen/api"
	"github.com/99designs/gqlgen/codegen/config"
	"github.com/99designs/gqlgen/plugin/modelgen"
)

const usage = `Usage:
  go run . [command]

Commands:
  fetch       Fetch Shopify introspection and write schema.graphql
  generate    Generate Go models from the existing schema.graphql
  all         Fetch schema.graphql, then generate Go models (default)
  help        Show this help
`

func main() {
	command := "all"
	if len(os.Args) > 1 {
		command = strings.ToLower(strings.TrimSpace(os.Args[1]))
	}

	var apiVersion *string
	if len(os.Args) > 2 {
		apiVersion = &os.Args[2]
	}

	var err error
	exitcode := 1
	switch command {
	case "fetch":
		exitcode, err = fetchSchema(apiVersion)
	case "generate":
		exitcode, err = generateModels()
	case "all":
		exitcode, err = fetchAndGenerate(apiVersion)
	case "help", "-h", "--help":
		fmt.Print(usage)
		return
	default:
		exitcode = 2
		err = fmt.Errorf("unknown command %q\n\n%s", command, usage)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitcode)
	}
}

func fetchAndGenerate(apiVersion *string) (int, error) {

	fmt.Println("Starting Fetch")
	exitcode, err := fetchSchema(apiVersion)
	if err != nil {
		return exitcode, fmt.Errorf("fetch schema: %w", err)
	}
	fmt.Println("Starting Generate")
	exitcode, err = generateModels()
	if err != nil {
		return exitcode, fmt.Errorf("generate models: %w", err)
	}

	return 0, nil
}

func generateModels() (int, error) {
	cfg, err := config.LoadConfigFromDefaultLocations()
	if err != nil {
		return 2, fmt.Errorf("failed to load gqlgen load config: %w", err.Error())
	}

	// Attaching the mutation function onto modelgen plugin.
	p := modelgen.Plugin{
		MutateHook: mutateHook,
	}

	err = api.Generate(cfg,
		api.NoPlugins(),
		api.AddPlugin(&p),
	)
	if err != nil {
		return 3, err
	}

	version, err := os.ReadFile("./files/apiVersion.txt")
	if err != nil {
		fmt.Fprintln(os.Stderr, "get apiVersion:", err)
		version = []byte("unknown")
	}
	if f, err := os.Create("./graph/model/version.go"); err != nil {
		fmt.Fprintln(os.Stderr, "save version.go:", err)
	} else {
		defer f.Close()
		_, err := f.WriteString(fmt.Sprintf("package model\n\nconst VERSION string = \"%s\"\n", version))
		if err != nil {
			fmt.Fprintln(os.Stderr, "save apiVersion:", err)
		}
	}

	return 0, nil
}

func isPointer(v interface{}) bool {
	_, ok := v.(*types.Pointer)

	return ok
}

func isSlice(v interface{}) bool {
	_, ok := v.(*types.Slice)

	return ok
}

// Defining mutation function.
func mutateHook(b *modelgen.ModelBuild) *modelgen.ModelBuild {
	for _, model := range b.Models {
		for _, field := range model.Fields {
			if !strings.Contains(field.Tag, ",omitempty") && (isPointer(field.Type) || isSlice(field.Type)) {
				tag := strings.TrimSuffix(field.Tag, `"`)
				field.Tag = fmt.Sprintf(`%v,omitempty"`, tag)
			}
		}
	}

	return b
}

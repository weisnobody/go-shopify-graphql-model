package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// do clean version of notes
const DO_CLEAN bool = true
// include SDL header
const SDL_ACTUAL bool = false

// unauthenticated Shopify GraphQL endpoint
const PROXY_URL string = "https://shopify.dev/admin-graphql-direct-proxy"

const introspectionQuery = `
query IntrospectionQuery {
  __schema {
    queryType { name }
    mutationType { name }
    subscriptionType { name }
    types {
      ...FullType
    }
    directives {
      name
      description
      locations
      args(includeDeprecated: true) {
        ...InputValue
      }
      isRepeatable
    }
  }
}

fragment FullType on __Type {
  kind
  name
  description
  specifiedByURL
  fields(includeDeprecated: true) {
    name
    description
    args(includeDeprecated: true) {
      ...InputValue
    }
    type { ...TypeRef }
    isDeprecated
    deprecationReason
  }
  inputFields(includeDeprecated: true) {
    ...InputValue
  }
  interfaces { ...TypeRef }
  enumValues(includeDeprecated: true) {
    name
    description
    isDeprecated
    deprecationReason
  }
  possibleTypes { ...TypeRef }
}

fragment InputValue on __InputValue {
  name
  description
  type { ...TypeRef }
  defaultValue
  isDeprecated
  deprecationReason
}

fragment TypeRef on __Type {
  kind
  name
  ofType {
    kind
    name
    ofType {
      kind
      name
      ofType {
        kind
        name
        ofType {
          kind
          name
          ofType {
            kind
            name
            ofType {
              kind
              name
              ofType {
                kind
                name
              }
            }
          }
        }
      }
    }
  }
}
`

type graphQLRequest struct {
	Query string `json:"query"`
}

type graphQLResponse struct {
	Data   introspectionData `json:"data"`
	Errors []graphQLError    `json:"errors"`
}

type graphQLError struct {
	Message string `json:"message"`
}

type introspectionData struct {
	Schema schema `json:"__schema"`
}

type schema struct {
	QueryType        *namedType  `json:"queryType"`
	MutationType     *namedType  `json:"mutationType"`
	SubscriptionType *namedType  `json:"subscriptionType"`
	Types            []fullType  `json:"types"`
	Directives       []directive `json:"directives"`
}

type namedType struct {
	Name string `json:"name"`
}

type fullType struct {
	Kind           string       `json:"kind"`
	Name           string       `json:"name"`
	Description    *string      `json:"description"`
	SpecifiedByURL *string      `json:"specifiedByURL"`
	Fields         []field      `json:"fields"`
	InputFields    []inputValue `json:"inputFields"`
	Interfaces     []typeRef    `json:"interfaces"`
	EnumValues     []enumValue  `json:"enumValues"`
	PossibleTypes  []typeRef    `json:"possibleTypes"`
}

type field struct {
	Name              string       `json:"name"`
	Description       *string      `json:"description"`
	Args              []inputValue `json:"args"`
	Type              typeRef      `json:"type"`
	IsDeprecated      bool         `json:"isDeprecated"`
	DeprecationReason *string      `json:"deprecationReason"`
}

type inputValue struct {
	Name              string  `json:"name"`
	Description       *string `json:"description"`
	Type              typeRef `json:"type"`
	DefaultValue      *string `json:"defaultValue"`
	IsDeprecated      bool    `json:"isDeprecated"`
	DeprecationReason *string `json:"deprecationReason"`
}

type enumValue struct {
	Name              string  `json:"name"`
	Description       *string `json:"description"`
	IsDeprecated      bool    `json:"isDeprecated"`
	DeprecationReason *string `json:"deprecationReason"`
}

type directive struct {
	Name         string       `json:"name"`
	Description  *string      `json:"description"`
	Locations    []string     `json:"locations"`
	Args         []inputValue `json:"args"`
	IsRepeatable bool         `json:"isRepeatable"`
}

type typeRef struct {
	Kind   string   `json:"kind"`
	Name   *string  `json:"name"`
	OfType *typeRef `json:"ofType"`
}

func fetchSchema(apiVersion *string) (int, error) {

	if apiVersion == nil {
		apiVersionEnv, err := requiredEnv("API_VERSION")
		if err != nil {
			return 5, err
		} else if apiVersion == nil {
			apiVersion = &apiVersionEnv
		}
	}

	accessToken := ""
	endpoint := ""

	store, err := requiredEnv("STORE")
	if err != nil || store == "" {
		endpoint = fmt.Sprintf("%s/%s", PROXY_URL, *apiVersion)
	} else {
		accessToken, err = requiredEnv("ACCESS_TOKEN")
		if err != nil {
			return 5, err
		}
		endpoint = fmt.Sprintf(
			"https://%s.myshopify.com/admin/api/%s/graphql.json",
			store,
			*apiVersion,
		)
	}

	s, err := getSchema(endpoint, accessToken)
	if err != nil {
		return 6, err
	}


	outputFile := "./files/schema.graphql"
	if SDL_ACTUAL {
		outputFile = "./files/schema.sdl"
	}
	if err := os.WriteFile(outputFile, []byte(printSchema(s)), 0o644); err != nil {
		return 7, fmt.Errorf("write %s: %w", outputFile, err)
	}

	// save the API returned from the shopify
	if f, err := os.Create("./files/apiVersion.txt"); err != nil {
		fmt.Fprintln(os.Stderr, "save apiVersion:", err)
	} else {
		defer f.Close()
		_, err := f.WriteString(*apiVersion)
		if err != nil {
			fmt.Fprintln(os.Stderr, "save apiVersion:", err)
		}
	}

	return 0, nil
}

func requiredEnv(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("environment variable %s is required", name)
	}
	return value, nil
}

func getSchema(endpoint, accessToken string) (schema, error) {
	payload, err := json.Marshal(graphQLRequest{Query: introspectionQuery})
	if err != nil {
		return schema{}, fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return schema{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if accessToken != "" {
		req.Header.Set("X-Shopify-Access-Token", accessToken)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return schema{}, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return schema{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return schema{}, fmt.Errorf("Shopify returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	// save the JSON returned from the shopify
	f, err := os.Create("./files/schema.json")
	if err != nil {
		fmt.Fprintln(os.Stderr, "save json schema:", err)
	} else {
		defer f.Close()
		_, err := f.WriteString(string(body))
		if err != nil {
			fmt.Fprintln(os.Stderr, "save json schema:", err)
		}
	}

	var result graphQLResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return schema{}, fmt.Errorf("decode response: %w", err)
	}

	if len(result.Errors) > 0 {
		messages := make([]string, 0, len(result.Errors))
		for _, gqlErr := range result.Errors {
			messages = append(messages, gqlErr.Message)
		}
		return schema{}, errors.New("GraphQL errors: " + strings.Join(messages, "; "))
	}

	if len(result.Data.Schema.Types) == 0 {
		return schema{}, errors.New("introspection response contained no schema types")
	}

	return result.Data.Schema, nil
}

func printSchema(s schema) string {
	var out strings.Builder

	if SDL_ACTUAL {
		writeSchemaDefinition(&out, s)
	}

	directives := append([]directive(nil), s.Directives...)
	sort.Slice(directives, func(i, j int) bool { return directives[i].Name < directives[j].Name })
	for _, d := range directives {
		if isSpecifiedDirective(d.Name) {
			continue
		}
		if out.Len() > 0 {
			out.WriteString("\n\n")
		}
		writeDirective(&out, d)
	}

	types := append([]fullType(nil), s.Types...)
	sort.Slice(types, func(i, j int) bool { return types[i].Name < types[j].Name })
	for _, t := range types {
		if t.Name == "" || strings.HasPrefix(t.Name, "__") || isSpecifiedScalar(t.Name) {
			continue
		}
		if out.Len() > 0 {
			out.WriteString("\n\n")
		}
		writeType(&out, t)
	}

	out.WriteByte('\n')
	return out.String()
}

func writeSchemaDefinition(out *strings.Builder, s schema) {
	queryName := typeName(s.QueryType)
	mutationName := typeName(s.MutationType)
	subscriptionName := typeName(s.SubscriptionType)

	// GraphQL's printSchema omits the schema block when the root type names are conventional.
	if queryName == "Query" && (mutationName == "" || mutationName == "Mutation") && (subscriptionName == "" || subscriptionName == "Subscription") {
		return
	}

	out.WriteString("schema {\n")
	if queryName != "" {
		fmt.Fprintf(out, "  query: %s\n", queryName)
	}
	if mutationName != "" {
		fmt.Fprintf(out, "  mutation: %s\n", mutationName)
	}
	if subscriptionName != "" {
		fmt.Fprintf(out, "  subscription: %s\n", subscriptionName)
	}
	out.WriteString("}")
}

func typeName(t *namedType) string {
	if t == nil {
		return ""
	}
	return t.Name
}

func writeType(out *strings.Builder, t fullType) {
	writeDescription(out, "", t.Description)

	switch t.Kind {
	case "SCALAR":
		fmt.Fprintf(out, "scalar %s", t.Name)
		if t.SpecifiedByURL != nil && *t.SpecifiedByURL != "" {
			fmt.Fprintf(out, " @specifiedBy(url: %s)", quoteGraphQLString(*t.SpecifiedByURL))
		}

	case "OBJECT":
		fmt.Fprintf(out, "type %s", t.Name)
		writeImplements(out, t.Interfaces)
		out.WriteString(" {\n")
		for i, f := range t.Fields {
			if i > 0 && f.Description != nil {
				out.WriteByte('\n')
			}
			writeDescription(out, "  ", f.Description)
			fmt.Fprintf(out, "  %s", f.Name)
			writeArgs(out, f.Args, "  ")
			fmt.Fprintf(out, ": %s%s\n", renderTypeRef(f.Type), deprecationSuffix(f.IsDeprecated, f.DeprecationReason))
		}
		out.WriteString("}")

	case "INTERFACE":
		fmt.Fprintf(out, "interface %s", t.Name)
		writeImplements(out, t.Interfaces)
		out.WriteString(" {\n")
		for i, f := range t.Fields {
			if i > 0 && f.Description != nil {
				out.WriteByte('\n')
			}
			writeDescription(out, "  ", f.Description)
			fmt.Fprintf(out, "  %s", f.Name)
			writeArgs(out, f.Args, "  ")
			fmt.Fprintf(out, ": %s%s\n", renderTypeRef(f.Type), deprecationSuffix(f.IsDeprecated, f.DeprecationReason))
		}
		out.WriteString("}")

	case "UNION":
		fmt.Fprintf(out, "union %s", t.Name)
		if len(t.PossibleTypes) > 0 {
			parts := make([]string, 0, len(t.PossibleTypes))
			for _, p := range t.PossibleTypes {
				parts = append(parts, renderTypeRef(p))
			}
			out.WriteString(" = " + strings.Join(parts, " | "))
		}

	case "ENUM":
		fmt.Fprintf(out, "enum %s {\n", t.Name)
		for i, v := range t.EnumValues {
			if i > 0 && v.Description != nil {
				out.WriteByte('\n')
			}
			writeDescription(out, "  ", v.Description)
			fmt.Fprintf(out, "  %s%s\n", v.Name, deprecationSuffix(v.IsDeprecated, v.DeprecationReason))
		}
		out.WriteString("}")

	case "INPUT_OBJECT":
		fmt.Fprintf(out, "input %s {\n", t.Name)
		for i, f := range t.InputFields {
			if i > 0 && f.Description != nil {
				out.WriteByte('\n')
			}
			writeDescription(out, "  ", f.Description)
			fmt.Fprintf(out, "  %s: %s", f.Name, renderTypeRef(f.Type))
			if f.DefaultValue != nil {
				fmt.Fprintf(out, " = %s", *f.DefaultValue)
			}
			out.WriteString(deprecationSuffix(f.IsDeprecated, f.DeprecationReason))
			out.WriteByte('\n')
		}
		out.WriteString("}")
	}
}

func writeImplements(out *strings.Builder, interfaces []typeRef) {
	if len(interfaces) == 0 {
		return
	}
	parts := make([]string, 0, len(interfaces))
	for _, iface := range interfaces {
		parts = append(parts, renderTypeRef(iface))
	}
	out.WriteString(" implements " + strings.Join(parts, " & "))
}

func writeArgs(out *strings.Builder, args []inputValue, indent string) {
	if len(args) == 0 {
		return
	}

	hasDescription := false
	for _, arg := range args {
		if arg.Description != nil {
			hasDescription = true
			break
		}
	}

	if !hasDescription {
		out.WriteByte('(')
		for i, arg := range args {
			if i > 0 {
				out.WriteString(", ")
			}
			writeInputValueInline(out, arg)
		}
		out.WriteByte(')')
		return
	}

	out.WriteString("(\n")
	for _, arg := range args {
		writeDescription(out, indent+"  ", arg.Description)
		out.WriteString(indent + "  ")
		writeInputValueInline(out, arg)
		out.WriteByte('\n')
	}
	out.WriteString(indent + ")")
}

func writeInputValueInline(out *strings.Builder, v inputValue) {
	fmt.Fprintf(out, "%s: %s", v.Name, renderTypeRef(v.Type))
	if v.DefaultValue != nil {
		fmt.Fprintf(out, " = %s", *v.DefaultValue)
	}
	out.WriteString(deprecationSuffix(v.IsDeprecated, v.DeprecationReason))
}

func writeDirective(out *strings.Builder, d directive) {
	writeDescription(out, "", d.Description)
	fmt.Fprintf(out, "directive @%s", d.Name)
	writeArgs(out, d.Args, "")
	if d.IsRepeatable {
		out.WriteString(" repeatable")
	}
	out.WriteString(" on ")
	out.WriteString(strings.Join(d.Locations, " | "))
}

func renderTypeRef(t typeRef) string {
	switch t.Kind {
	case "NON_NULL":
		if t.OfType == nil {
			return ""
		}
		return renderTypeRef(*t.OfType) + "!"
	case "LIST":
		if t.OfType == nil {
			return "[]"
		}
		return "[" + renderTypeRef(*t.OfType) + "]"
	default:
		if t.Name == nil {
			return ""
		}
		return *t.Name
	}
}

func deprecationSuffix(deprecated bool, reason *string) string {
	if !deprecated {
		return ""
	}
	if reason == nil || *reason == "" || *reason == "No longer supported" {
		return " @deprecated"
	}
	return " @deprecated(reason: " + quoteGraphQLString(*reason) + ")"
}

func writeDescription(out *strings.Builder, indent string, description *string) {
	if description == nil || *description == "" {
		return
	}

	var text string
	// TrimSuffix is "better", but is different than the javascript version
	if DO_CLEAN {
		text = strings.TrimSuffix(*description, "\n")
	} else {
		text = *description
	}

	if !strings.ContainsAny(text, "\n\r") && !strings.Contains(text, `"""`) {
		if len(text) > 70 {
			out.WriteString(indent)
			out.WriteString("\"\"\"\n")
			out.WriteString(indent)
			out.WriteString(text)
			out.WriteByte('\n')
			out.WriteString(indent)
			out.WriteString("\"\"\"")
		} else {
			out.WriteString(indent)
			out.WriteString("\"\"\"")
			out.WriteString(string(text))
			out.WriteString("\"\"\"")
		}
		out.WriteByte('\n')
		return
	}

	// this makes it more closely match the javascript version, but IMO we should remove it
	// there is possibly a more correct criteria for matching when the javascript does this, but I couldn't figure it out
	// || strings.ContainsAny(text, "\"")
	if !DO_CLEAN {
		if strings.HasPrefix(text, "A filter made up of terms, connectives, modifiers, and comparators.") ||
			strings.HasPrefix(text, "A string containing a strict subset of HTML code. Non-allowed tags will be stripped out") ||
			strings.HasPrefix(text, "A string containing HTML code. Refer to the [HTML spec]") ||
			strings.HasPrefix(text, "A string containing a hexadecimal representation of a color.") ||
			strings.HasPrefix(text, "Represents an [ISO 8601](https://en.wikipedia.org/wiki/ISO_8601)-encoded") ||
			strings.HasPrefix(text, "A signed decimal number, which supports arbitrary precision and is serialized as a string") ||
			strings.HasPrefix(text, "Time between UTC time and a location's observed time, in the format") ||
			strings.HasPrefix(text, "A [JSON](https://www.json.org/json-en.html) object") ||
			strings.HasPrefix(text, "Represents a unique identifier in the Storefront API.") ||
			strings.HasPrefix(text, "Represents an [RFC 3986](https://datatracker.ietf.org/doc/html/rfc3986)") ||
			strings.HasPrefix(text, "An unsigned 64-bit integer. Represents whole numeric values between 0 and 2^64 - 1") {
			out.WriteString(indent)
			out.WriteString("\"")
			out.WriteString(strings.ReplaceAll(strings.ReplaceAll(text, "\"", "\\\""), "\n", "\\n"))
			out.WriteString("\"")
			out.WriteByte('\n')
			return
		}
	}

	out.WriteString(indent + `"""` + "\n")
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		out.WriteString(indent)
		out.WriteString(strings.ReplaceAll(line, `"""`, `\"""`))
		out.WriteByte('\n')
	}
	out.WriteString(indent + `"""` + "\n")
}

func quoteGraphQLString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func isSpecifiedScalar(name string) bool {
	switch name {
	case "String", "Boolean", "Int", "Float", "ID":
		return true
	default:
		return false
	}
}

func isSpecifiedDirective(name string) bool {
	switch name {
	case "skip", "include", "deprecated", "specifiedBy":
		return true
	default:
		return false
	}
}

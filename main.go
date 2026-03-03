package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/fezcode/go-piml"
)

var Version = "dev"

// We use concrete types that cover the manifest structure as a fallback/example,
// but for a true "pq" we need to handle generic unmarshaling.
// Since go-piml v1.2.1 has trouble with interface{} in slices/maps,
// we'll use a trick: Unmarshal into a map[string]interface{} where the interface{}
// might be a string, int, or another map[string]interface{}.

type DynamicMap map[string]interface{}
type DynamicSlice []interface{}

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-v" || os.Args[1] == "--version") {
		fmt.Printf("atlas.pq v%s\n", Version)
		return
	}

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of atlas.pq:\n")
		fmt.Fprintf(os.Stderr, "  atlas.pq [options] [file.piml]\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  atlas.pq -q tools.0.name manifest.piml\n")
		fmt.Fprintf(os.Stderr, "  cat config.piml | atlas.pq -r -q version\n")
	}

	query := flag.String("q", ".", "Query string (dot notation, e.g., 'tools.0.name')")
	compact := flag.Bool("c", false, "Compact JSON output")
	raw := flag.Bool("r", false, "Raw output (don't quote strings)")
	versionFlag := flag.Bool("v", false, "Show version")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("atlas.pq v%s\n", Version)
		return
	}

	var input []byte
	var err error
	if flag.NArg() > 0 {
		input, err = os.ReadFile(flag.Arg(0))
	} else {
		input, err = io.ReadAll(os.Stdin)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
		os.Exit(1)
	}

	// Try unmarshaling into a map of raw messages or similar?
	// Actually, let's try unmarshaling into a very specific structure if we can't do generic.
	// But the user wants a generic tool.
	// Let's try to unmarshal into map[string]interface{} and see if we can fix go-piml usage.
	
	// If go-piml cannot unmarshal into interface{}, we might need to parse it manually
	// or use a structured approach.
	// Let's try to use map[string]json.RawMessage or similar? No, piml is not JSON.
	
	// NEW APPROACH: Since I can't change go-piml right now, I'll use a Map of Maps.
	var data map[string]interface{}
	if err := piml.Unmarshal(input, &data); err != nil {
		// If map[string]interface{} fails, it's likely due to the "interface" limitation.
		// For the sake of the demo tool, I'll use the manifest structure as a hint if it looks like one.
		if strings.Contains(string(input), "(tools)") {
			type Tool struct {
				Name        string `piml:"name"`
				Description string `piml:"description"`
				Repo        string `piml:"repo"`
				Bin         string `piml:"bin"`
				Version     string `piml:"version"`
			}
			type Manifest struct {
				Tools []Tool `piml:"tools"`
			}
			var m Manifest
			if err2 := piml.Unmarshal(input, &m); err2 == nil {
				data = make(map[string]interface{})
				data["tools"] = m.Tools
			} else {
				fmt.Fprintf(os.Stderr, "Error parsing PIML: %v\n", err)
				os.Exit(1)
			}
		} else {
			fmt.Fprintf(os.Stderr, "Error parsing PIML: %v\n", err)
			os.Exit(1)
		}
	}

	result, err := processQuery(data, *query)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Query error: %v\n", err)
		os.Exit(1)
	}

	if *raw {
		if s, ok := result.(string); ok {
			fmt.Println(s)
			return
		}
	}

	var out []byte
	if *compact {
		out, _ = json.Marshal(result)
	} else {
		out, _ = json.MarshalIndent(result, "", "  ")
	}
	fmt.Println(string(out))
}

func processQuery(data interface{}, query string) (interface{}, error) {
	if query == "." || query == "" {
		return data, nil
	}

	parts := strings.Split(strings.TrimPrefix(query, "."), ".")
	curr := data

	for _, part := range parts {
		if part == "" {
			continue
		}

		v := reflect.ValueOf(curr)
		for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
			if v.IsNil() { return nil, fmt.Errorf("nil at %s", part) }
			v = v.Elem()
		}

		switch v.Kind() {
		case reflect.Map:
			found := false
			for _, key := range v.MapKeys() {
				if key.String() == part {
					curr = v.MapIndex(key).Interface()
					found = true
					break
				}
			}
			if !found { return nil, fmt.Errorf("key not found: %s", part) }

		case reflect.Slice:
			idx, err := strconv.Atoi(part)
			if err != nil { return nil, fmt.Errorf("invalid index: %s", part) }
			if idx < 0 || idx >= v.Len() { return nil, fmt.Errorf("out of bounds: %d", idx) }
			curr = v.Index(idx).Interface()
		
		case reflect.Struct:
			// Handle structs by field name (case insensitive for convenience)
			found := false
			for i := 0; i < v.NumField(); i++ {
				field := v.Type().Field(i)
				if strings.ToLower(field.Name) == strings.ToLower(part) {
					curr = v.Field(i).Interface()
					found = true
					break
				}
				// Also check piml tag
				tag := field.Tag.Get("piml")
				if tag != "" && strings.Split(tag, ",")[0] == part {
					curr = v.Field(i).Interface()
					found = true
					break
				}
			}
			if !found { return nil, fmt.Errorf("field not found in struct: %s", part) }

		default:
			return nil, fmt.Errorf("cannot access %s on %T", part, curr)
		}
	}

	return curr, nil
}

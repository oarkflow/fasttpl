package main

import (
	"fmt"
	"os"

	"github.com/oarkflow/fasttpl"
)

func main() {
	// Original string-based example
	fmt.Println("=== String-based Template Example ===")
	page, _ := fasttpl.Compile(`
<html>
<head><title>{{ title | trim | upper }}</title></head>
<body>
  {{ include "header" }}
  <ul>
  {{ range item in items }}
    <li>{{ $item.name }} — {{ $item.price }}</li>
  {{ end }}
  </ul>
  {{ if user.admin }}<div class="admin">Hi, {{ user.name }}</div>{{ else }}<div>Welcome!</div>{{ end }}
</body>
</html>`)

	header, _ := fasttpl.Compile(`<header>Hi {{ user.name | truncate:12 }}</header>`)
	page.RegisterPartial("header", header)

	out, _ := page.RenderString(map[string]any{
		"title": "  Products  ",
		"user":  map[string]any{"name": "Orgware", "admin": true},
		"items": []map[string]any{{"name": "Alpha", "price": 100}, {"name": "Beta", "price": 120}},
	})
	fmt.Println(out)

	// File-based rendering examples
	fmt.Println("\n=== File-based Template Examples ===")

	// Example 1: Basic file rendering
	fmt.Println("\n--- Basic File Rendering ---")

	// Create a sample template file
	templateContent := `
<html>
<head><title>{{ title }}</title></head>
<body>
	<h1>{{ title }}</h1>
	<p>Welcome, {{ user.name }}!</p>
	{{ if user.admin }}
	<div class="admin-panel">Admin access granted</div>
	{{ end }}
</body>
</html>`

	// Write template to file
	err := os.WriteFile("sample_template.html", []byte(templateContent), 0644)
	if err != nil {
		fmt.Printf("Error creating template file: %v\n", err)
		return
	}
	defer os.Remove("sample_template.html") // Clean up

	// Compile and render the template file
	tmpl, err := fasttpl.CompileFile("sample_template.html")
	if err != nil {
		fmt.Printf("Error compiling template: %v\n", err)
		return
	}

	data := map[string]any{
		"title": "My Website",
		"user": map[string]any{
			"name":  "John Doe",
			"admin": true,
		},
	}

	result, err := tmpl.RenderString(data)
	if err != nil {
		fmt.Printf("Error rendering template: %v\n", err)
		return
	}

	fmt.Println(result)

	// Example 2: File rendering with includes
	fmt.Println("\n--- File Rendering with Includes ---")

	// Create main template
	mainTemplate := `
<html>
<head><title>{{ title }}</title></head>
<body>
	{{ include "header" }}
	<div class="content">
		<h1>{{ content.title }}</h1>
		<p>{{ content.text }}</p>
	</div>
	{{ include "footer" }}
</body>
</html>`

	// Create partial templates
	headerTemplate := `<header><nav>Home | About | Contact</nav></header>`
	footerTemplate := `<footer>&copy; 2025 My Company</footer>`

	// Write files
	err = os.WriteFile("main_template.html", []byte(mainTemplate), 0644)
	if err != nil {
		fmt.Printf("Error creating main template: %v\n", err)
		return
	}
	defer os.Remove("main_template.html")

	err = os.WriteFile("_header.html", []byte(headerTemplate), 0644)
	if err != nil {
		fmt.Printf("Error creating header: %v\n", err)
		return
	}
	defer os.Remove("_header.html")

	err = os.WriteFile("_footer.html", []byte(footerTemplate), 0644)
	if err != nil {
		fmt.Printf("Error creating footer: %v\n", err)
		return
	}
	defer os.Remove("_footer.html")

	// Compile with includes (auto-discovers partials)
	tmplWithIncludes, err := fasttpl.CompileFile("main_template.html")
	if err != nil {
		fmt.Printf("Error compiling template with includes: %v\n", err)
		return
	}

	data2 := map[string]any{
		"title": "Page with Includes",
		"content": map[string]any{
			"title": "Welcome",
			"text":  "This page demonstrates template includes.",
		},
	}

	result2, err := tmplWithIncludes.RenderString(data2)
	if err != nil {
		fmt.Printf("Error rendering template with includes: %v\n", err)
		return
	}

	fmt.Println(result2)

	// Example 3: Rendering to file
	fmt.Println("\n--- Rendering to File ---")

	outputFile, err := os.Create("output.html")
	if err != nil {
		fmt.Printf("Error creating output file: %v\n", err)
		return
	}
	defer outputFile.Close()
	defer os.Remove("output.html")

	err = tmpl.Render(outputFile, data)
	if err != nil {
		fmt.Printf("Error rendering to file: %v\n", err)
		return
	}

	fmt.Println("Template rendered to output.html")

	// Read back the file to show the result
	content, err := os.ReadFile("output.html")
	if err != nil {
		fmt.Printf("Error reading output file: %v\n", err)
		return
	}

	fmt.Println("Contents of output.html:")
	fmt.Println(string(content))

	// Example 4: Custom delimiters
	fmt.Println("\n--- Custom Delimiters ---")

	customTpl, err := fasttpl.Compile(`<html><body><h1><< title >></h1><p><< user.name >></p></body></html>`, fasttpl.WithDelims("<<", ">>"))
	if err != nil {
		fmt.Printf("Error compiling with custom delimiters: %v\n", err)
		return
	}

	customResult, err := customTpl.RenderString(data)
	if err != nil {
		fmt.Printf("Error rendering with custom delimiters: %v\n", err)
		return
	}

	fmt.Println("Custom delimiters result:")
	fmt.Println(customResult)
}

# FastTpl

A high-performance, zero-allocation template engine for Go that compiles templates into optimized renderers.

[![Go Version](https://img.shields.io/badge/go-1.25+-blue.svg)](https://golang.org)

## Features

- 🚀 **High Performance**: Optimized for speed with zero-allocation rendering paths
- 📦 **Template Compilation**: Pre-compile templates for maximum performance
- 🔄 **Caching**: Built-in file and compile caching with modification time checking
- 🎯 **Rich Syntax**: Variables, conditionals, loops, includes, and filters
- 🛡️ **HTML Escaping**: Automatic HTML escaping with raw output support
- 🎨 **Custom Delimiters**: Support for custom template delimiters
- 🔧 **Extensible Filters**: Built-in filters with support for custom ones
- 📁 **File-based Templates**: Load templates from files with automatic include discovery
- 🏗️ **Template Pools**: Reusable template pools for hot paths
- ⚡ **Fast Reflection**: Cached reflection with precomputed field access

## Installation

```bash
go get github.com/oarkflow/fasttpl
```

## Quick Start

```go
package main

import (
    "fmt"
    "fasttpl"
)

func main() {
    // Compile a template
    tmpl, err := fasttpl.Compile(`
<html>
<head><title>{{ title }}</title></head>
<body>
    <h1>{{ title }}</h1>
    <p>Welcome, {{ user.name }}!</p>
    {{ if user.admin }}
    <div class="admin">Admin access granted</div>
    {{ end }}
</body>
</html>`)

    if err != nil {
        panic(err)
    }

    // Render with data
    data := map[string]any{
        "title": "My Website",
        "user": map[string]any{
            "name":  "John Doe",
            "admin": true,
        },
    }

    result, err := tmpl.RenderString(data)
    if err != nil {
        panic(err)
    }

    fmt.Println(result)
}
```

## Template Syntax

### Variables

Access data using dot notation:

```go
{{ user.name }}
{{ user.address.city }}
{{ items.0.name }}
```

### Conditionals

```go
{{ if user.admin }}
<div class="admin">Admin panel</div>
{{ else }}
<div>Welcome!</div>
{{ end }}
```

### Loops

```go
{{ range item in items }}
<li>{{ $item.name }} - {{ $item.price }}</li>
{{ end }}
```

### Includes

```go
{{ include "header" }}
```

### Filters

```go
{{ title | trim | upper }}
{{ description | truncate:100 }}
```

### Raw Output

```go
{{ raw htmlContent }}
```

### Local Variables

```go
{{ let fullName = user.firstName + " " + user.lastName }}
<p>Hello, {{ $fullName }}!</p>
```

## API Reference

### Core Functions

#### `Compile(src string, opts ...Option) (*Template, error)`

Compiles a template string into a high-performance renderer.

```go
tmpl, err := fasttpl.Compile("Hello, {{ name }}!")
```

#### `CompileFile(filename string, opts ...Option) (*Template, error)`

Compiles a template from a file with automatic include discovery.

```go
tmpl, err := fasttpl.CompileFile("template.html")
```

#### `CompileCached(src string, opts ...Option) (*Template, error)`

Compiles a template with in-memory caching.

```go
tmpl, err := fasttpl.CompileCached("Hello, {{ name }}!")
```

### Template Methods

#### `(*Template) Render(w io.Writer, data any) error`

Renders the template to an `io.Writer`.

```go
err := tmpl.Render(os.Stdout, data)
```

#### `(*Template) RenderString(data any) (string, error)`

Renders the template and returns a string.

```go
result, err := tmpl.RenderString(data)
```

#### `(*Template) RenderToBytes(data any) ([]byte, error)`

Renders the template to a byte slice with zero allocations.

```go
result, err := tmpl.RenderToBytes(data)
```

#### `(*Template) RegisterPartial(name string, partial *Template)`

Registers a named partial template for includes.

```go
header, _ := fasttpl.Compile("<header>Hi!</header>")
tmpl.RegisterPartial("header", header)
```

### Options

#### `WithFilters(filters Filters)`

Sets custom filters.

```go
customFilters := fasttpl.Filters{
    "reverse": func(s string, args []string) (string, error) {
        runes := []rune(s)
        for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
            runes[i], runes[j] = runes[j], runes[i]
        }
        return string(runes), nil
    },
}

tmpl, err := fasttpl.Compile(src, fasttpl.WithFilters(customFilters))
```

#### `WithDelims(left, right string)`

Sets custom delimiters.

```go
tmpl, err := fasttpl.Compile(src, fasttpl.WithDelims("<<", ">>"))
```

### Caching

#### File Cache

```go
// Use global file cache
tmpl, err := fasttpl.CompileFile("template.html")

// Or create custom cache
cache := fasttpl.NewFileCache(500)
tmpl, err := cache.CompileFile("template.html")
```

#### Compile Cache

```go
// Use global compile cache
tmpl, err := fasttpl.CompileCached("template source")

// Or create custom cache
cache := &fasttpl.CompileCache{MaxSize: 1000}
tmpl, err := cache.Compile("template source")
```

### Template Pools

For high-performance scenarios with frequently used templates:

```go
pool, err := fasttpl.NewTemplatePool("Hello, {{ name }}!")
if err != nil {
    panic(err)
}

result, err := pool.RenderString(map[string]any{"name": "World"})
```

## Built-in Filters

- `upper`: Converts string to uppercase
- `lower`: Converts string to lowercase
- `trim`: Trims whitespace
- `truncate:n`: Truncates string to n characters

## Examples

### Basic Template

```go
tmpl, _ := fasttpl.Compile(`
<html>
<head><title>{{ title }}</title></head>
<body>
    <h1>{{ title }}</h1>
    <p>Welcome, {{ user.name }}!</p>
</body>
</html>`)

data := map[string]any{
    "title": "My Website",
    "user": map[string]any{"name": "John"},
}

result, _ := tmpl.RenderString(data)
fmt.Println(result)
```

### File-based Templates

```go
// Create template file
templateContent := `
<html>
<head><title>{{ title }}</title></head>
<body>
    <h1>{{ title }}</h1>
    <p>Welcome, {{ user.name }}!</p>
</body>
</html>`

os.WriteFile("template.html", []byte(templateContent), 0644)

// Compile and render
tmpl, err := fasttpl.CompileFile("template.html")
if err != nil {
    panic(err)
}

data := map[string]any{
    "title": "My Website",
    "user": map[string]any{"name": "John"},
}

result, err := tmpl.RenderString(data)
```

### Templates with Includes

```go
// Main template
mainTmpl := `
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

// Partial templates
headerTmpl := `<header><nav>Home | About | Contact</nav></header>`
footerTmpl := `<footer>&copy; 2025 My Company</footer>`

// Write files
os.WriteFile("main.html", []byte(mainTmpl), 0644)
os.WriteFile("_header.html", []byte(headerTmpl), 0644)
os.WriteFile("_footer.html", []byte(footerTmpl), 0644)

// Compile with automatic include discovery
tmpl, err := fasttpl.CompileFile("main.html")

data := map[string]any{
    "title": "Page with Includes",
    "content": map[string]any{
        "title": "Welcome",
        "text": "This page demonstrates includes.",
    },
}

result, err := tmpl.RenderString(data)
```

### Rendering to File

```go
tmpl, _ := fasttpl.Compile("Hello, {{ name }}!")

file, err := os.Create("output.html")
if err != nil {
    panic(err)
}
defer file.Close()

err = tmpl.Render(file, map[string]any{"name": "World"})
```

### Custom Delimiters

```go
tmpl, _ := fasttpl.Compile(`
<html>
<body>
    <h1><< title >></h1>
    <p><< user.name >></p>
</body>
</html>`, fasttpl.WithDelims("<<", ">>"))

result, _ := tmpl.RenderString(data)
```

### Advanced Data Structures

```go
type User struct {
    Name  string
    Email string
    Admin bool
}

type Product struct {
    Name  string
    Price float64
}

data := struct {
    User     User
    Products []Product
}{
    User: User{Name: "John", Email: "john@example.com", Admin: true},
    Products: []Product{
        {Name: "Widget", Price: 19.99},
        {Name: "Gadget", Price: 29.99},
    },
}

tmpl, _ := fasttpl.Compile(`
<h1>Welcome, {{ user.name }}</h1>
{{ if user.admin }}
<div class="admin-notice">You have admin privileges</div>
{{ end }}

<h2>Products</h2>
<ul>
{{ range product in products }}
    <li>{{ $product.name }} - ${{ $product.price }}</li>
{{ end }}
</ul>
`)

result, _ := tmpl.RenderString(data)
```

### Custom Filters

```go
customFilters := fasttpl.Filters{
    "currency": func(s string, args []string) (string, error) {
        if len(args) == 0 {
            return "$" + s, nil
        }
        return args[0] + s, nil
    },
    "pluralize": func(s string, args []string) (string, error) {
        count, err := strconv.Atoi(s)
        if err != nil {
            return s, nil
        }
        if len(args) < 2 {
            return s, nil
        }
        if count == 1 {
            return "1 " + args[0]
        }
        return s + " " + args[1]
    },
}

tmpl, _ := fasttpl.Compile(`
<p>Price: {{ price | currency }}</p>
<p>Items: {{ count | pluralize:item:items }}</p>
`, fasttpl.WithFilters(customFilters))

data := map[string]any{
    "price": "19.99",
    "count": "3",
}

result, _ := tmpl.RenderString(data)
// Output: <p>Price: $19.99</p><p>Items: 3 items</p>
```

## Performance Features

### Zero-Allocation Rendering

FastTpl is designed for high performance with several optimizations:

- **Object Pooling**: Reuses buffers, contexts, and other objects
- **Fast Reflection**: Cached reflection with precomputed field indices
- **String Optimization**: Zero-copy string conversions where possible
- **HTML Escaping**: Optimized HTML escaping with minimal allocations

### Benchmarking

```go
// Warm up caches for accurate benchmarking
fasttpl.WarmupCache()

// Benchmark rendering
err := tmpl.RenderToDiscard(data) // Renders to io.Discard
```

### Precomputing Field Access

For better performance with known struct types:

```go
type User struct {
    Name  string
    Email string
}

tmpl.PrecomputeFieldAccess(reflect.TypeOf(User{}))
```

## File Structure

FastTpl automatically discovers partial templates in the same directory as the main template. Files starting with `_` are treated as partials:

```
templates/
├── main.html
├── _header.html
├── _footer.html
└── _sidebar.html
```

When compiling `main.html`, FastTpl will automatically register `_header.html` as "header", `_footer.html` as "footer", etc.

## Error Handling

FastTpl provides detailed error messages for template compilation and rendering:

```go
tmpl, err := fasttpl.Compile("Hello, {{ undefinedVar }}")
if err != nil {
    fmt.Println("Compilation error:", err)
}

err = tmpl.RenderString(data)
if err != nil {
    fmt.Println("Rendering error:", err)
}
```

## Thread Safety

- Templates are safe for concurrent use
- Caches are thread-safe
- Template pools are thread-safe

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Performance Comparison

FastTpl is designed to be significantly faster than Go's standard `html/template` package, especially for:

- Large datasets
- Complex templates
- Frequent rendering of the same template
- High-concurrency scenarios

For benchmark results, see `benchmark_test.go`.

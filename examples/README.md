# FastTpl Auto-Reload Server Example

This example demonstrates FastTpl's automatic template reloading feature with a complete web server.

## Features

- 🚀 **Auto-Reload**: Templates automatically reload when files are modified
- 🌐 **Web Server**: Complete HTTP server with template rendering
- 📁 **Template Includes**: Demonstrates template inheritance and partials
- 🔄 **Live Development**: Edit templates and see changes instantly
- 📊 **Status Endpoint**: JSON API for server status

## Quick Start

```bash
# Build and run the server
go run main.go

# Or build first
go build -o server main.go
./server
```

The server will start at `http://localhost:8080`

## What It Does

1. **Creates Sample Templates**: Automatically creates a `templates/` directory with sample HTML templates
2. **Watches for Changes**: Monitors template files for modifications
3. **Auto-Reloads**: Recompiles templates when files change
4. **Serves Pages**: Renders templates with sample data

## Template Structure

```
templates/
├── layout.html      # Main layout with includes
├── index.html       # Home page content
├── about.html       # About page content
├── _header.html     # Header partial
└── _footer.html     # Footer partial
```

## Endpoints

- `GET /` - Home page
- `GET /about` - About page
- `GET /status` - Server status (JSON)
- `GET /templates/*` - Serve template files statically

## Try It Out

1. Start the server: `go run main.go`
2. Open `http://localhost:8080` in your browser
3. Edit any template file (e.g., `templates/index.html`)
4. Refresh the page to see changes instantly!

## Example Template Changes

Try editing `templates/index.html` and change:

```html
<h2>Welcome to {{ site.name }}</h2>
```

to:

```html
<h2>Welcome to the Amazing {{ site.name }}</h2>
```

Then refresh `http://localhost:8080` to see the change!

## Server Features

- **FastTpl Integration**: Uses all FastTpl features
- **Error Handling**: Graceful error handling for template issues
- **Logging**: Logs template reload events
- **Static File Serving**: Serves template files for inspection
- **JSON Status API**: RESTful endpoint for server information

## Development Workflow

1. Edit template files in your editor
2. Save changes
3. Refresh browser
4. See changes immediately (no server restart required!)

This makes FastTpl perfect for rapid template development and prototyping.

package fasttpl

import (
	"html/template"
	"io"
	"strings"
	"testing"
)

var (
	fastTpl *Template
	htmlTpl *template.Template
	data    = map[string]any{
		"title": "  Products  ",
		"user":  map[string]any{"name": "Orgware", "admin": true},
		"items": []map[string]any{{"name": "Alpha", "price": 100}, {"name": "Beta", "price": 120}},
	}
)

func init() {
	var err error
	fastTpl, err = Compile(`
<html>
<head><title>{{ title | trim | upper }}</title></head>
<body>
  <ul>
  {{ range item in items }}
    <li>{{ $item.name }} — {{ $item.price }}</li>
  {{ end }}
  </ul>
  {{ if user.admin }}<div class="admin">Hi, {{ user.name }}</div>{{ else }}<div>Welcome!</div>{{ end }}
</body>
</html>`)
	if err != nil {
		panic(err)
	}

	htmlTpl, err = template.New("test").Funcs(template.FuncMap{
		"trim":  fastTrim,
		"upper": strings.ToUpper,
	}).Parse(`
<html>
<head><title>{{.Title | trim | upper }}</title></head>
<body>
  <ul>
  {{range .Items}}
    <li>{{.Name}} — {{.Price}}</li>
  {{end}}
  </ul>
  {{if .User.Admin}}<div class="admin">Hi, {{.User.Name}}</div>{{else}}<div>Welcome!</div>{{end}}
</body>
</html>`)
	if err != nil {
		panic(err)
	}
}

func BenchmarkFastTpl(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = fastTpl.Render(io.Discard, data)
	}
}

func BenchmarkHTMLTemplate(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = htmlTpl.Execute(io.Discard, struct {
			Title string
			User  struct {
				Name  string
				Admin bool
			}
			Items []struct {
				Name  string
				Price int
			}
		}{
			Title: "  Products  ",
			User: struct {
				Name  string
				Admin bool
			}{Name: "Orgware", Admin: true},
			Items: []struct {
				Name  string
				Price int
			}{{Name: "Alpha", Price: 100}, {Name: "Beta", Price: 120}},
		})
	}
}

func TestWithFeature(t *testing.T) {
	tpl, err := Compile(`{{ with user }}{{ name }}{{ end }}`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := tpl.RenderString(data)
	if err != nil {
		t.Fatal(err)
	}
	expected := "Orgware"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

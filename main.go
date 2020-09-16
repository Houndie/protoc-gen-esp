package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/pluginpb"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
		os.Exit(1)
	}
}

func run() error {
	input, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("Could not read contents from stdin: %w", err)
	}

	req := &pluginpb.CodeGeneratorRequest{}
	if err := proto.Unmarshal(input, req); err != nil {
		return fmt.Errorf("Error unmarshaling proto request: %w", err)
	}

	resp, err := generateResponse(req)
	if err != nil {
		return err
	}

	data, err := proto.Marshal(resp)
	if err != nil {
		return fmt.Errorf("error marshaling proto response: %w", err)
	}

	if _, err := os.Stdout.Write(data); err != nil {
		return fmt.Errorf("error writing proto response to stdout: %w", err)
	}

	return nil
}

func generateResponse(req *pluginpb.CodeGeneratorRequest) (*pluginpb.CodeGeneratorResponse, error) {
	resp := &pluginpb.CodeGeneratorResponse{}
	for _, file := range req.ProtoFile {
		buf := bytes.Buffer{}
		err := tmpl.Execute(&buf, file)
		if err != nil {
			return nil, fmt.Errorf("error executing template: %w", err)
		}

		result := strings.TrimSpace(buf.String())
		if result != "" {
			resp.File = append(resp.File, &pluginpb.CodeGeneratorResponse_File{
				Name:    proto.String(strings.TrimSuffix(file.GetName(), filepath.Ext(file.GetName())) + ".js"),
				Content: proto.String(string(result)),
			})
		}
	}
	return resp, nil
}

var tmpl = template.Must(template.New("").Funcs(template.FuncMap{
	"TrimPrefix":  strings.TrimPrefix,
	"DerefString": func(i *string) string { return *i },
	"Subtract":    func(a, b int) int { return a - b },
	"dict": func(values ...interface{}) (map[string]interface{}, error) {
		if len(values)%2 != 0 {
			return nil, errors.New("invalid dict call")
		}
		dict := make(map[string]interface{}, len(values)/2)
		for i := 0; i < len(values); i += 2 {
			key, ok := values[i].(string)
			if !ok {
				return nil, errors.New("dict keys must be strings")
			}
			dict[key] = values[i+1]
		}
		return dict, nil
	},
}).Parse(
	` 
{{ define "DefineEnum" }}
{{.Indent}}{{ .Enum.Name }} = {
{{- range $i, $v := .Enum.Value }}
{{$.Indent}}	{{ $v.Name }}: "{{ $v.Name }}"{{ if ne (Subtract (len $.Enum.Value) 1) $i }},{{ end }}
{{- end }}
{{.Indent}}}
{{ end }}
{{ range .Service }}
{{ $servicePin := . }}
class {{ .Name }}Client {
	constructor(server, options) {
		this.server = server
		this.options = options
	}

	{{ range .Method }}
	{{ .Name }}(req, options) {
		return fetch(this.server+"/{{ $.Package }}.{{ $servicePin.Name }}/{{ .Name }}", {
			method: 'POST',
    		body: JSON.stringify(req),
    		headers: {
        		'Content-Type': 'application/json',
				...options.headers,
				...this.options.headers
			}
    	}).then((response) => {
			return new {{ DerefString $.Package | printf ".%s." | TrimPrefix .OutputType }}(JSON.parse(response.text))
		})
	}
	{{ end }}
}
{{ end }}

{{ range .EnumType }}
{{ template "DefineEnum" dict "Enum" . "Indent" ""}}
{{ end }}

{{ range .MessageType }}
class {{ .Name }} {
	{{ range .EnumType }}
	{{ template "DefineEnum" dict "Enum" . "Indent" "\t"}}
	{{ end }}
	constructor(o) {
		{{- range .Field }}
			this._{{.JsonName}} = o.{{.JsonName}}
		{{- end }}
	}

	{{ range .Field }}
	get {{ .JsonName }}() {
		return this._{{ .JsonName }} 
	}

	set {{ .JsonName }}({{ .JsonName }}) {
		this._{{ .JsonName	}} = {{ .JsonName }}
	}
	{{ end }}
	toJSON() {
		obj := new Object()
		{{ range .Field -}}
		{{ if not .Proto3Optional }}
		if this._{{ .JsonName }} === undefined {
			throw 'Field {{ .JsonName }} is both undefined and not optional'
		}
		{{- end }}
		obj.{{ .JsonName }} = this._{{ .JsonName }}
		{{ end }}
		return obj
	}
}
{{ end }}
	`,
))

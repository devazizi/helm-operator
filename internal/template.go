package internal

import (
	"bytes"
	"fmt"
	"gopkg.in/yaml.v3"
	"text/template"
)

func renderValuesAsTemplate(values interface{}, vars map[string]interface{}) (interface{}, error) {
	values, err := renderValues(values, vars)
	if err != nil {
		return nil, fmt.Errorf("failed to render values: %w", err)
	}

	return values, nil
}

func renderValues(values interface{}, vars map[string]interface{}) ([]byte, error) {
	valuesYAML, err := yaml.Marshal(values)
	if err != nil {
		return nil, err
	}

	tmpl, err := template.New("values").Parse(string(valuesYAML))
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

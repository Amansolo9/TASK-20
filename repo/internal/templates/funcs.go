package templates

import (
	"fmt"
	"html/template"
)

// FuncMap returns custom template functions
func FuncMap() template.FuncMap {
	return template.FuncMap{
		"divf": func(a int64, b float64) float64 {
			return float64(a) / b
		},
		"deref": func(p *uint) uint {
			if p == nil {
				return 0
			}
			return *p
		},
		"printf": func(format string, a ...interface{}) string {
			return fmt.Sprintf(format, a...)
		},
	}
}

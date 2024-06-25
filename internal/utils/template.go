package utils

import (
	"fmt"
	"regexp"
)

func ReplaceCustomFields(str string, fields map[string]string) string {
	for key, value := range fields {
		regExp := fmt.Sprintf(`\{\{ *%s *\}\}`, key)
		reg, err := regexp.Compile(regExp)
		if err != nil {
			continue
		}
		str = reg.ReplaceAllString(str, value)
	}
	return str
}

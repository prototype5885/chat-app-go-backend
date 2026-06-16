package validator

import (
	"regexp"
)

type Schema struct {
	Field  string
	Min    int
	Max    int
	Regexp *regexp.Regexp
}

type ValidationError struct {
	Field  string   `json:"field"`
	Issues []string `json:"issues"`
}

const (
	TokenLength = 43
)

var SpecialCharsRegexp = regexp.MustCompile(`^[a-zA-Z0-9]+$`)

var UsernameSchema = Schema{Field: "username", Min: 6, Max: 32, Regexp: SpecialCharsRegexp}
var PasswordSchema = Schema{Field: "password", Min: 6, Max: 128, Regexp: nil}
var DisplaynameSchema = Schema{Field: "display_name", Min: 1, Max: 64, Regexp: nil}
var CustomStatusSchema = Schema{Field: "custom_status", Min: 1, Max: 32, Regexp: nil}
var ServerNameSchema = Schema{Field: "server_name", Min: 1, Max: 64, Regexp: nil}
var ChannelNameSchema = Schema{Field: "channel_name", Min: 1, Max: 32, Regexp: nil}
var TextMessageSchema = Schema{Field: "text_message", Min: 1, Max: 4096, Regexp: nil}

func (schema *Schema) Validate(text string, optional bool) ValidationError {
	issues := []string{}

	if text == "" {
		if optional {
			return ValidationError{}
		}
		issues = append(issues, "empty")
	} else if len(text) < schema.Min {
		issues = append(issues, "short")
	} else if len(text) > schema.Max {
		issues = append(issues, "long")
	}

	if text != "" && schema.Regexp != nil && !schema.Regexp.MatchString(text) {
		issues = append(issues, "no_special_chars")
	}

	if len(issues) == 0 {
		return ValidationError{}
	}

	return ValidationError{
		Field:  schema.Field,
		Issues: issues,
	}
}

func MergeValidationIssues(allIssues ...ValidationError) []ValidationError {
	issues := []ValidationError{}

	for i := range allIssues {
		if allIssues[i].Issues != nil {
			issues = append(issues, allIssues[i])
		}
	}

	return issues
}

package model

import (
	"fmt"
)

const (
	Markdown  = "markdown"
	PlainText = "plaintext"
)

var OutputFormatUnitMap = map[string]string{
	"markdown":   Markdown,
	"Markdown":   Markdown,
	"MarkDown":   Markdown,
	"mark_down":  Markdown,
	"md":         Markdown,
	"mdn":        Markdown,
	"mkd":        Markdown,
	"plaintext":  PlainText,
	"plain_text": PlainText,
	"PlainText":  PlainText,
	"Plaintext":  PlainText,
	"text":       PlainText,
	"txt":        PlainText,
}

type OutputFormat string

func Ext2OutputFormat(extension string) string {
	if unit, ok := OutputFormatUnitMap[extension]; ok {
		return unit
	} else {
		return PlainText
	}
}

func (m *OutputFormat) Set(value string) error {
	if unit, ok := OutputFormatUnitMap[value]; ok {
		*m = OutputFormat(unit)
		return nil
	} else {
		return fmt.Errorf("invalid value: %q. Allowed values are 'markdown', 'plaintext'", value)

	}
}

func (m *OutputFormat) String() string {
	return string(*m)
}

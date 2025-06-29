package model

import (
	"fmt"
)

const (
	Markdown  = "markdown"
	PlainText = "plaintext"
	XML       = "xml"
	Arklite   = "arklite"
	Auto      = "auto"
)

var OutputFormatUnitMap = map[string]string{
	"markdown":   Markdown,
	"Markdown":   Markdown,
	"MarkDown":   Markdown,
	"mark_down":  Markdown,
	"mark-down":  Markdown,
	"md":         Markdown,
	"mdn":        Markdown,
	"mkd":        Markdown,
	"plaintext":  PlainText,
	"plain_text": PlainText,
	"plain-text": PlainText,
	"PlainText":  PlainText,
	"Plaintext":  PlainText,
	"text":       PlainText,
	"txt":        PlainText,
	"xml":        XML,
	"Xml":        XML,
	"XML":        XML,
	"arklite":    Arklite,
	"arkl":       Arklite,
	"al":         Arklite,
	"compact":    Arklite,
	"auto":       Auto,
}

var OutputFormatAllowComplessMap = map[string]bool{
	Markdown:  true,
	PlainText: true,
	XML:       true,
	Arklite:   false,
	Auto:      false,
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
		return fmt.Errorf("invalid value: %q. Allowed values are 'markdown', 'plaintext', 'xml', 'auto'", value)

	}
}

func (m *OutputFormat) CanCompless() bool {
	return OutputFormatAllowComplessMap[m.String()]
}

func (m *OutputFormat) String() string {
	return string(*m)
}

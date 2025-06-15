package core

import (
	"fmt"
	"strings"

	"github.com/magicdrive/ark/internal/model"
	"github.com/magicdrive/ark/internal/textbank"
)

// PrependDescriptionWithFormat prepends a descriptive header suitable for AI processing in either plain text or markdown format.
func PrependDescriptionWithFormat(projectName, root string, format model.OutputFormat) string {
	projectName = strings.TrimSpace(projectName)
	root = strings.TrimSpace(root)

	var header string

	switch format.String() {
	case model.Markdown:
		header = fmt.Sprintf(textbank.DescriptionTemplateMarkdown, projectName, root)

	case model.PlainText:
		header = fmt.Sprintf(textbank.DescriptionTemplateText, projectName, root)
	default:
		header = "" // fallback to no header
	}

	return header
}

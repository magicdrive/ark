package textbank

import _ "embed"

//go:embed description_template/description.md
var DescriptionTemplateMarkdown string

//go:embed description_template/description.txt
var DescriptionTemplateText string

//go:embed description_template/description.xml
var DescriptionTemplateXML string

//go:embed description_template/description.arklite
var DescriptionTemplateArklite string

const (
	EmojiSuccess     = "✅"
	EmojiInterrupted = "🛑"
	EmojiAlmost      = "🔒"
	EmojiDone        = "🎉"
	EmojiArchive     = "📜"
	EmojiArk         = "🪨"
	EmojiBoard       = "🪧"
	EmojiStar        = "🌟"
	EmojiHourglass   = "⏳"
)

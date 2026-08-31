package matrix

import (
	"bytes"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

const htmlFormat = "org.matrix.custom.html"

var markdown = goldmark.New(goldmark.WithExtensions(extension.GFM))

func messageContent(body string) Content {
	var buf bytes.Buffer
	if err := markdown.Convert([]byte(body), &buf); err != nil {
		return Content{MsgType: "m.text", Body: body}
	}
	return Content{
		MsgType:       "m.text",
		Body:          body,
		Format:        htmlFormat,
		FormattedBody: buf.String(),
	}
}

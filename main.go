package main

/*
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

typedef struct {
    char* html;
    int word_count;
    char* error;
} ParseResult;
*/
import "C"
import (
	"encoding/json"
	"fmt"
	"strings"
	"unsafe"
)

type MarkdownParser struct {
	builderPool chan *strings.Builder
}

func NewMarkdownParser() *MarkdownParser {
	pool := make(chan *strings.Builder, 10)
	for i := 0; i < 10; i++ {
		pool <- &strings.Builder{}
	}

	return &MarkdownParser{
		builderPool: pool,
	}
}

func (mp *MarkdownParser) getBuilder(estimatedSize int) *strings.Builder {
	select {
	case builder := <-mp.builderPool:
		builder.Reset()
		if builder.Cap() < estimatedSize {
			builder.Grow(estimatedSize - builder.Cap())
		}
		return builder
	default:
		builder := &strings.Builder{}
		builder.Grow(estimatedSize)
		return builder
	}
}

func (mp *MarkdownParser) returnBuilder(builder *strings.Builder) {
	if builder.Cap() < 64*1024 { // 64KB max
		select {
		case mp.builderPool <- builder:
		default:
		}
	}
}

func (mp *MarkdownParser) Parse(markdown string) string {
	inputSize := len(markdown)
	estimatedSize := mp.estimateHTMLSize(markdown, inputSize)

	builder := mp.getBuilder(estimatedSize)
	defer mp.returnBuilder(builder)

	lines := strings.Split(markdown, "\n")
	inCodeBlock := false

	for _, line := range lines {
		line = strings.TrimRight(line, " \t")

		if strings.HasPrefix(line, "```") {
			if inCodeBlock {
				builder.WriteString("</code></pre>\n")
				inCodeBlock = false
			} else {
				lang := strings.TrimPrefix(line, "```")
				if lang == "" {
					builder.WriteString("<pre><code>")
				} else {
					fmt.Fprintf(builder, "<pre><code class=\"language-%s\">", lang)
				}
				inCodeBlock = true
			}
			continue
		}

		if inCodeBlock {
			builder.WriteString(line)
			builder.WriteString("\n")
			continue
		}

		switch {
		case strings.HasPrefix(line, "# "):
			text := strings.TrimPrefix(line, "# ")
			fmt.Fprintf(builder, "<h1>%s</h1>\n", text)

		case strings.HasPrefix(line, "## "):
			text := strings.TrimPrefix(line, "## ")
			fmt.Fprintf(builder, "<h2>%s</h2>\n", text)

		case strings.HasPrefix(line, "### "):
			text := strings.TrimPrefix(line, "### ")
			fmt.Fprintf(builder, "<h3>%s</h3>\n", text)

		case strings.HasPrefix(line, "#### "):
			text := strings.TrimPrefix(line, "#### ")
			fmt.Fprintf(builder, "<h4>%s</h4>\n", text)

		case strings.HasPrefix(line, "##### "):
			text := strings.TrimPrefix(line, "##### ")
			fmt.Fprintf(builder, "<h5>%s</h5>\n", text)

		case strings.HasPrefix(line, "###### "):
			text := strings.TrimPrefix(line, "###### ")
			fmt.Fprintf(builder, "<h6>%s</h6>\n", text)

		case strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* "):
			text := line[2:]
			fmt.Fprintf(builder, "<li>%s</li>\n", mp.processInlineFormatting(text))

		case strings.HasPrefix(line, "> "):
			text := strings.TrimPrefix(line, "> ")
			fmt.Fprintf(builder, "<blockquote>%s</blockquote>\n", text)

		case strings.TrimSpace(line) == "":

		default:
			if strings.TrimSpace(line) != "" {
				fmt.Fprintf(builder, "<p>%s</p>\n", mp.processInlineFormatting(line))
			}
		}
	}

	return builder.String()
}

func (mp *MarkdownParser) estimateHTMLSize(markdown string, inputSize int) int {
	estimate := inputSize * 2

	headingCount := strings.Count(markdown, "#")
	linkCount := strings.Count(markdown, "](")
	boldCount := strings.Count(markdown, "**")
	italicCount := strings.Count(markdown, "*")
	codeBlockCount := strings.Count(markdown, "```")

	estimate += headingCount * 10   // <h1></h1> vs #
	estimate += linkCount * 20      // <a href="..."></a> vs [](url)
	estimate += boldCount * 15      // <strong></strong> vs **
	estimate += italicCount * 10    // <em></em> vs *
	estimate += codeBlockCount * 30 // <pre><code></code></pre> vs ```

	estimate = int(float64(estimate) * 1.2)

	if estimate < 512 {
		estimate = 512
	} else if estimate > 1024*1024 {
		estimate = 1024 * 1024
	}

	return estimate
}

func (mp *MarkdownParser) processInlineFormatting(text string) string {
	// **bold**
	for strings.Contains(text, "**") {
		start := strings.Index(text, "**")
		if start == -1 {
			break
		}
		end := strings.Index(text[start+2:], "**")
		if end == -1 {
			break
		}
		end += start + 2

		bold := text[start+2 : end]
		replacement := fmt.Sprintf("<strong>%s</strong>", bold)
		text = text[:start] + replacement + text[end+2:]
	}

	// *italic*
	for strings.Contains(text, "*") && !strings.Contains(text, "**") {
		start := strings.Index(text, "*")
		if start == -1 {
			break
		}
		end := strings.Index(text[start+1:], "*")
		if end == -1 {
			break
		}
		end += start + 1

		italic := text[start+1 : end]
		replacement := fmt.Sprintf("<em>%s</em>", italic)
		text = text[:start] + replacement + text[end+1:]
	}

	// [text](url) - links
	for strings.Contains(text, "](") {
		linkStart := strings.Index(text, "[")
		if linkStart == -1 {
			break
		}

		linkTextEnd := strings.Index(text[linkStart:], "](")
		if linkTextEnd == -1 {
			break
		}
		linkTextEnd += linkStart

		linkEnd := strings.Index(text[linkTextEnd+2:], ")")
		if linkEnd == -1 {
			break
		}
		linkEnd += linkTextEnd + 2

		linkText := text[linkStart+1 : linkTextEnd]
		linkURL := text[linkTextEnd+2 : linkEnd]
		replacement := fmt.Sprintf("<a href=\"%s\">%s</a>", linkURL, linkText)
		text = text[:linkStart] + replacement + text[linkEnd+1:]
	}

	// `inline code`
	for strings.Contains(text, "`") {
		start := strings.Index(text, "`")
		if start == -1 {
			break
		}
		end := strings.Index(text[start+1:], "`")
		if end == -1 {
			break
		}
		end += start + 1

		code := text[start+1 : end]
		replacement := fmt.Sprintf("<code>%s</code>", code)
		text = text[:start] + replacement + text[end+1:]
	}

	return text
}

func countWords(text string) int {
	words := strings.Fields(text)
	return len(words)
}

// ===== C Functions exports  =====

//export ParseMarkdown
func ParseMarkdown(input *C.char) *C.char {
	if input == nil {
		return C.CString(`{"error": "null input"}`)
	}

	markdown := C.GoString(input)
	parser := NewMarkdownParser()
	html := parser.Parse(markdown)
	wordCount := countWords(markdown)

	result := map[string]interface{}{
		"html":       html,
		"word_count": wordCount,
		"error":      nil,
	}

	jsonBytes, _ := json.Marshal(result)
	return C.CString(string(jsonBytes))
}

//export ParseMarkdownWithOptions
func ParseMarkdownWithOptions(input *C.char, options *C.char) *C.char {
	if input == nil {
		return C.CString(`{"error": "null input"}`)
	}

	markdown := C.GoString(input)

	_ = C.GoString(options)

	parser := NewMarkdownParser()
	html := parser.Parse(markdown)
	wordCount := countWords(markdown)

	result := map[string]interface{}{
		"html":       html,
		"word_count": wordCount,
		"error":      nil,
	}

	jsonBytes, _ := json.Marshal(result)
	return C.CString(string(jsonBytes))
}

//export FreeString
func FreeString(ptr *C.char) {
	if ptr != nil {
		C.free(unsafe.Pointer(ptr))
	}
}

//export GetParserInfo
func GetParserInfo() *C.char {
	info := map[string]interface{}{
		"version":     "1.0.0",
		"features":    []string{"headings", "lists", "bold", "italic", "links", "code", "blockquotes"},
		"backend":     "custom-go-parser",
		"performance": "optimized",
	}

	jsonBytes, _ := json.Marshal(info)
	return C.CString(string(jsonBytes))
}

//export ParseMarkdownBatch
func ParseMarkdownBatch(inputArray **C.char, arraySize C.int) *C.char {
	if inputArray == nil || arraySize <= 0 {
		return C.CString(`{"error": "invalid input array"}`)
	}

	inputs := (*[1 << 28]*C.char)(unsafe.Pointer(inputArray))[:arraySize:arraySize]

	parser := NewMarkdownParser()
	results := make([]map[string]interface{}, arraySize)

	for i, cStr := range inputs {
		if cStr != nil {
			markdown := C.GoString(cStr)
			html := parser.Parse(markdown)
			wordCount := countWords(markdown)

			results[i] = map[string]interface{}{
				"html":       html,
				"word_count": wordCount,
				"index":      i,
				"error":      nil,
			}
		} else {
			results[i] = map[string]interface{}{
				"error": fmt.Sprintf("null input at index %d", i),
				"index": i,
			}
		}
	}

	jsonBytes, _ := json.Marshal(results)
	return C.CString(string(jsonBytes))
}

func main() {
}

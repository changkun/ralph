package main

import (
	"fmt"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// Agents write Markdown. The app parses it into blocks, laid out by
// layoutMarkdown, each holding runs of styled text.

// mdSpan is a run of text in one style.
type mdSpan struct {
	text   string
	bold   bool
	italic bool
	code   bool
	strike bool
	link   bool
}

type mdKind uint8

const (
	mdParagraph mdKind = iota
	mdHeading
	mdCode
	mdRule
	mdTable
)

// mdBlock is a paragraph, heading, code block, rule, or table.
type mdBlock struct {
	kind   mdKind
	level  int    // heading level, 1 to 6
	indent int    // list nesting depth
	marker string // list marker, on the first block of a list item
	quote  int    // blockquote nesting depth
	spans  []mdSpan
	code   string
	rows   [][][]mdSpan // table rows of cells, the header first
}

var markdown = goldmark.New(goldmark.WithExtensions(extension.Table, extension.Strikethrough))

// parseMarkdown splits src into blocks.
func parseMarkdown(src string) []mdBlock {
	p := &mdParser{src: []byte(src)}
	p.blocks(markdown.Parser().Parse(text.NewReader(p.src)), mdContext{})
	return p.out
}

type mdParser struct {
	src []byte
	out []mdBlock
}

// mdContext is where a block sits: in which lists and quotes.
type mdContext struct {
	indent, quote int
	marker        string
}

func (p *mdParser) blocks(parent ast.Node, c mdContext) {
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		p.block(n, c)
		c.marker = "" // only an item's first block shows its marker
	}
}

func (p *mdParser) add(b mdBlock, c mdContext) {
	b.indent, b.marker, b.quote = c.indent, c.marker, c.quote
	p.out = append(p.out, b)
}

func (p *mdParser) block(n ast.Node, c mdContext) {
	switch n := n.(type) {
	case *ast.Paragraph, *ast.TextBlock:
		p.add(mdBlock{kind: mdParagraph, spans: p.inline(n)}, c)
	case *ast.Heading:
		p.add(mdBlock{kind: mdHeading, level: n.Level, spans: p.inline(n)}, c)
	case *ast.FencedCodeBlock, *ast.CodeBlock, *ast.HTMLBlock:
		p.add(mdBlock{kind: mdCode, code: p.lines(n)}, c)
	case *ast.ThematicBreak:
		p.add(mdBlock{kind: mdRule}, c)
	case *ast.Blockquote:
		c.quote++
		p.blocks(n, c)
	case *ast.List:
		i := 0
		for item := n.FirstChild(); item != nil; item = item.NextSibling() {
			marker := "•"
			if c.indent%2 == 1 {
				marker = "◦"
			}
			if n.IsOrdered() {
				marker = fmt.Sprintf("%d.", n.Start+i)
			}
			p.blocks(item, mdContext{indent: c.indent + 1, quote: c.quote, marker: marker})
			i++
		}
	case *east.Table:
		var rows [][][]mdSpan
		for row := n.FirstChild(); row != nil; row = row.NextSibling() {
			var cells [][]mdSpan
			for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
				cells = append(cells, p.inline(cell))
			}
			rows = append(rows, cells)
		}
		p.add(mdBlock{kind: mdTable, rows: rows}, c)
	default:
		p.blocks(n, c)
	}
}

// lines returns the raw text of a block such as a code block.
func (p *mdParser) lines(n ast.Node) string {
	var b strings.Builder
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		b.Write(seg.Value(p.src))
	}
	return strings.TrimRight(b.String(), "\n")
}

// inline returns the styled text inside n.
func (p *mdParser) inline(n ast.Node) []mdSpan {
	var spans []mdSpan
	emit := func(s mdSpan) {
		if s.text == "" {
			return
		}
		if last := len(spans) - 1; last >= 0 {
			if prev := spans[last]; prev.bold == s.bold && prev.italic == s.italic && prev.code == s.code && prev.strike == s.strike && prev.link == s.link {
				spans[last].text += s.text
				return
			}
		}
		spans = append(spans, s)
	}
	var walk func(n ast.Node, style mdSpan)
	walk = func(n ast.Node, style mdSpan) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			s := style
			switch c := c.(type) {
			case *ast.Text:
				s.text = string(c.Segment.Value(p.src))
				switch {
				case c.HardLineBreak():
					s.text += "\n"
				case c.SoftLineBreak():
					s.text += " "
				}
				emit(s)
			case *ast.String:
				s.text = string(c.Value)
				emit(s)
			case *ast.CodeSpan:
				s.code = true
				var b strings.Builder
				for t := c.FirstChild(); t != nil; t = t.NextSibling() {
					if t, ok := t.(*ast.Text); ok {
						b.Write(t.Segment.Value(p.src))
					}
				}
				s.text = b.String()
				emit(s)
			case *ast.Emphasis:
				if c.Level >= 2 {
					s.bold = true
				} else {
					s.italic = true
				}
				walk(c, s)
			case *ast.Link:
				s.link = true
				walk(c, s)
			case *ast.AutoLink:
				s.link, s.text = true, string(c.Label(p.src))
				emit(s)
			case *east.Strikethrough:
				s.strike = true
				walk(c, s)
			case *ast.RawHTML:
				segs := c.Segments
				for i := 0; i < segs.Len(); i++ {
					seg := segs.At(i)
					s.text = string(seg.Value(p.src))
					emit(s)
				}
			default:
				walk(c, s)
			}
		}
	}
	walk(n, mdSpan{})
	return spans
}

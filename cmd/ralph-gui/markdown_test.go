package main

import (
	"reflect"
	"testing"
)

func TestParseMarkdown(t *testing.T) {
	src := "# Plan\n\n" +
		"**Objective:** add a *small* test for `MODE_REGISTRY`, see https://x.dev.\n" +
		"Second line.\n\n" +
		"1. First\n" +
		"   - nested ~~old~~\n" +
		"2. [Second](https://example.com)\n\n" +
		"> quoted\n\n" +
		"```go\nfmt.Println(1)\n```\n\n" +
		"| A | B |\n|---|---|\n| 1 | **2** |\n\n" +
		"---\n"
	got := parseMarkdown(src)
	want := []mdBlock{
		{kind: mdHeading, level: 1, spans: []mdSpan{{text: "Plan"}}},
		{kind: mdParagraph, spans: []mdSpan{
			{text: "Objective:", bold: true},
			{text: " add a "},
			{text: "small", italic: true},
			{text: " test for "},
			{text: "MODE_REGISTRY", code: true},
			{text: ", see https://x.dev. Second line."},
		}},
		{kind: mdParagraph, indent: 1, marker: "1.", spans: []mdSpan{{text: "First"}}},
		{kind: mdParagraph, indent: 2, marker: "◦", spans: []mdSpan{{text: "nested "}, {text: "old", strike: true}}},
		{kind: mdParagraph, indent: 1, marker: "2.", spans: []mdSpan{{text: "Second", link: true}}},
		{kind: mdParagraph, quote: 1, spans: []mdSpan{{text: "quoted"}}},
		{kind: mdCode, code: "fmt.Println(1)"},
		{kind: mdTable, rows: [][][]mdSpan{
			{{{text: "A"}}, {{text: "B"}}},
			{{{text: "1"}}, {{text: "2", bold: true}}},
		}},
		{kind: mdRule},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d blocks, want %d:\n%+v", len(got), len(want), got)
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("block %d:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
}

func TestParseMarkdownListItemParagraphs(t *testing.T) {
	got := parseMarkdown("- one\n\n  more about one\n- two\n")
	markers := []string{}
	for _, b := range got {
		markers = append(markers, b.marker)
	}
	if want := []string{"•", "", "•"}; !reflect.DeepEqual(markers, want) {
		t.Errorf("markers = %q, want %q", markers, want)
	}
}

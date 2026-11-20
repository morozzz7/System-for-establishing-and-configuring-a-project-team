package handler

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// ExtractTextFromFile extracts plain text from supported document formats (.txt, .md, .json, .docx, .pdf).
func ExtractTextFromFile(filename string, data []byte) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".docx":
		if text, err := extractTextFromDocx(data); err == nil && len(strings.TrimSpace(text)) > 0 {
			return text
		}
	case ".pdf":
		if text := extractTextFromSimplePDF(data); len(strings.TrimSpace(text)) > 0 {
			return text
		}
	}
	// Default: if data is valid UTF-8, treat as plain text
	if utf8.Valid(data) {
		return string(data)
	}
	return extractPrintableASCII(data)
}

func extractTextFromDocx(data []byte) (string, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	for _, f := range r.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				return "", err
			}
			defer rc.Close()
			return parseDocxXML(rc)
		}
	}
	return "", fmt.Errorf("word/document.xml not found")
}

func parseDocxXML(r io.Reader) (string, error) {
	decoder := xml.NewDecoder(r)
	var paragraphs []string
	var cur strings.Builder
	inText := false

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch se := tok.(type) {
		case xml.StartElement:
			if se.Name.Local == "t" {
				inText = true
			}
		case xml.EndElement:
			if se.Name.Local == "t" {
				inText = false
			} else if se.Name.Local == "p" {
				pText := strings.TrimSpace(cur.String())
				if pText != "" {
					paragraphs = append(paragraphs, pText)
				}
				cur.Reset()
			}
		case xml.CharData:
			if inText {
				cur.Write(se)
			}
		}
	}
	if pText := strings.TrimSpace(cur.String()); pText != "" {
		paragraphs = append(paragraphs, pText)
	}
	return strings.Join(paragraphs, "\n"), nil
}

func extractTextFromSimplePDF(data []byte) string {
	var sb strings.Builder
	s := string(data)
	inParen := false
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '(' && !inParen {
			inParen = true
			cur.Reset()
		} else if c == ')' && inParen {
			inParen = false
			str := cur.String()
			if len(str) > 1 && utf8.ValidString(str) {
				sb.WriteString(str)
				sb.WriteString(" ")
			}
		} else if inParen {
			if c == '\\' && i+1 < len(s) {
				i++
				cur.WriteByte(s[i])
			} else {
				cur.WriteByte(c)
			}
		}
	}
	return strings.TrimSpace(sb.String())
}

func extractPrintableASCII(data []byte) string {
	var sb strings.Builder
	for _, b := range data {
		if (b >= 32 && b <= 126) || b == '\n' || b == '\t' {
			sb.WriteByte(b)
		}
	}
	return strings.TrimSpace(sb.String())
}

package handler_test

import (
	"archive/zip"
	"bytes"
	"testing"

	"github.com/teamopt/backend/internal/handler"
)

func TestExtractTextFromFile_PlainText(t *testing.T) {
	content := []byte("Техническое задание на разработку платформы.")
	extracted := handler.ExtractTextFromFile("brief.txt", content)
	if extracted != string(content) {
		t.Fatalf("expected %q, got %q", string(content), extracted)
	}

	mdContent := []byte("# Проект\n\nТребуется Frontend разработчик.")
	extractedMD := handler.ExtractTextFromFile("readme.md", mdContent)
	if extractedMD != string(mdContent) {
		t.Fatalf("expected %q, got %q", string(mdContent), extractedMD)
	}
}

func TestExtractTextFromFile_Docx(t *testing.T) {
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	xmlData := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:r><w:t>Разработка нового мобильного приложения.</w:t></w:r></w:p>
    <w:p><w:r><w:t>Срок 6 месяцев, стек React Native.</w:t></w:r></w:p>
  </w:body>
</w:document>`
	if _, err := w.Write([]byte(xmlData)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	extracted := handler.ExtractTextFromFile("project.docx", buf.Bytes())
	if extracted != "Разработка нового мобильного приложения.\nСрок 6 месяцев, стек React Native." {
		t.Fatalf("unexpected docx extraction: %q", extracted)
	}
}

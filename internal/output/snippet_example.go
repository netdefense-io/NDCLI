package output

import (
	"fmt"
	"io"

	"github.com/netdefense-io/NDCLI/internal/models"
)

func exampleSubtype(e models.SnippetExample) string {
	if e.Subtype == nil || *e.Subtype == "" {
		return "-"
	}
	return *e.Subtype
}

// printExampleContent writes the content alone on the writer: it is meant to
// be redirected into a file, so the docs path stays off it.
func printExampleContent(w io.Writer, e *models.SnippetExample) error {
	content, err := e.ContentJSON()
	if err != nil {
		return err
	}
	fmt.Fprintln(w, content)
	return nil
}

func printExampleDocs(w io.Writer, docs string) {
	if docs != "" {
		fmt.Fprintf(w, "\nDocs: %s\n", docs)
	}
}

// FormatSnippetExamples lists a type's examples.
func (f *TableFormatter) FormatSnippetExamples(snippetType, docs string, examples []models.SnippetExample) error {
	if len(examples) == 0 {
		f.Info("No examples for " + snippetType)
		return nil
	}
	table := NewStyledTable([]string{"ID", "Name", "Subtype"})
	for _, e := range examples {
		table.Append([]string{e.ID, e.Name, exampleSubtype(e)})
	}
	table.Render()
	printExampleDocs(f.Writer, docs)
	return nil
}

// FormatSnippetExample prints the example's content, ready for --file.
func (f *TableFormatter) FormatSnippetExample(snippetType, docs string, e *models.SnippetExample) error {
	return printExampleContent(f.Writer, e)
}

// FormatSnippetExamples lists a type's examples.
func (f *SimpleFormatter) FormatSnippetExamples(snippetType, docs string, examples []models.SnippetExample) error {
	if len(examples) == 0 {
		f.Info("No examples for " + snippetType)
		return nil
	}
	for _, e := range examples {
		fmt.Fprintf(f.Writer, "• %s (%s, subtype: %s)\n", e.ID, e.Name, exampleSubtype(e))
	}
	printExampleDocs(f.Writer, docs)
	return nil
}

// FormatSnippetExample prints the example's content, ready for --file.
func (f *SimpleFormatter) FormatSnippetExample(snippetType, docs string, e *models.SnippetExample) error {
	return printExampleContent(f.Writer, e)
}

// FormatSnippetExamples lists a type's examples.
func (f *DetailedFormatter) FormatSnippetExamples(snippetType, docs string, examples []models.SnippetExample) error {
	if len(examples) == 0 {
		f.Info("No examples for " + snippetType)
		return nil
	}
	for i, e := range examples {
		if i > 0 {
			fmt.Fprintln(f.Writer)
		}
		ColorHeader.Fprintf(f.Writer, "─── %s ───\n", e.ID)
		f.printLabelValue("Name", e.Name)
		f.printLabelValue("Subtype", exampleSubtype(e))
		if e.Requires != nil {
			for _, v := range e.Requires.Variables {
				f.printLabelValue("Needs variable", v)
			}
			for _, x := range e.Requires.Examples {
				f.printLabelValue("Needs example", x)
			}
		}
	}
	printExampleDocs(f.Writer, docs)
	return nil
}

// FormatSnippetExample prints the example's content, ready for --file.
func (f *DetailedFormatter) FormatSnippetExample(snippetType, docs string, e *models.SnippetExample) error {
	return printExampleContent(f.Writer, e)
}

// FormatSnippetExamples lists a type's examples as JSON.
func (f *JSONFormatter) FormatSnippetExamples(snippetType, docs string, examples []models.SnippetExample) error {
	return f.output(map[string]interface{}{
		"type":     snippetType,
		"docs":     docs,
		"examples": examples,
	})
}

// FormatSnippetExample prints the whole example object as JSON.
func (f *JSONFormatter) FormatSnippetExample(snippetType, docs string, e *models.SnippetExample) error {
	return f.output(struct {
		Type string `json:"type"`
		Docs string `json:"docs"`
		*models.SnippetExample
	}{snippetType, docs, e})
}

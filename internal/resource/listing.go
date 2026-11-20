package resource

import (
	"io"
	"text/tabwriter"
)

// Listing is a list of objects, ready to print. Items is a slice of the
// payload type and is never nil, so it prints as an empty list.
type Listing struct {
	Items any
	Len   int

	headers []string
	rows    func() [][]string
}

// WriteTable writes the items as a table: a header, then one row each.
func (l *Listing) WriteTable(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	writeRow(tw, l.headers)
	for _, row := range l.rows() {
		writeRow(tw, row)
	}
	return tw.Flush()
}

func writeRow(w io.Writer, cells []string) {
	for i, c := range cells {
		if i > 0 {
			_, _ = io.WriteString(w, "\t")
		}
		_, _ = io.WriteString(w, c)
	}
	_, _ = io.WriteString(w, "\n")
}

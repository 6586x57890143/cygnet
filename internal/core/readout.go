package core

import (
	"fmt"
	"strings"
	"time"

	"github.com/disgoorg/snowflake/v2"
)

// Reporter is a Module with live state worth showing its server's admins,
// on its page in /help: what it is connected to, what it has done, what
// failed. Optional, like Helper.
type Reporter interface {
	// Report is the module's state as it concerns guild, and nothing about
	// any other server. It is called on every page view, so it reads what
	// the module already keeps and makes no network call.
	Report(guild snowflake.ID) Report
}

// Report is facts for Readout and notes for the lines below them, in
// cygnet's voice (UX.md). An empty one shows nothing.
type Report struct {
	Rows  [][2]string
	Notes []string
}

// Readout lays rows out as a panel's facts (DESIGN.md): a line each, its
// label in bold; a record's are fields (brand.Fields). Plain text wraps the way any line does on whatever is reading it,
// where a code block grid cuts off on a phone and shows mentions as IDs.
func Readout(rows [][2]string) string {
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "**%s** %s\n", r[0], r[1])
	}
	return b.String()
}

// Duration is the two largest units that matter: "45s", "12m", "3h 12m",
// "2d 4h".
func Duration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
}

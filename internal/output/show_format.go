package output

import (
	"fmt"
	"io"
	"time"

	"github.com/fatih/color"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// WriteAuthShow is the human view of `auth show`: the stored session as
// classified locally, the host and the storage backend. now is when it is
// written, for the age of an expired token.
func WriteAuthShow(w io.Writer, a *models.AuthShow, now time.Time) {
	footer := func() {
		fmt.Fprintf(w, "  Host:      %s\n", a.Host)
		fmt.Fprintf(w, "  Storage:   %s\n", a.Storage)
	}
	expired := func() {
		fmt.Fprintf(w, "  Expired:   %s (%s)\n", expiresAt(a).Format(time.RFC3339), AgoSince(now, expiresAt(a)))
	}

	switch a.State {
	case models.SessionStatic:
		color.New(color.FgCyan).Fprintln(w, "Authentication Status:")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "  Method:    Personal access token (NDCLI_TOKEN)")
		footer()

	case models.SessionValid, models.SessionRefreshable:
		color.New(color.FgCyan).Fprintln(w, "Authentication Status:")
		fmt.Fprintln(w)
		if a.Email != "" {
			fmt.Fprintf(w, "  Email:     %s\n", a.Email)
		}
		if a.Name != "" {
			fmt.Fprintf(w, "  Name:      %s\n", a.Name)
		}
		if a.Subject != "" {
			fmt.Fprintf(w, "  Subject:   %s\n", a.Subject)
		}
		if a.State == models.SessionValid {
			fmt.Fprintf(w, "  Expires:   %s\n", expiresAt(a).Format(time.RFC3339))
			color.New(color.FgGreen).Fprintln(w, "  Status:    Valid")
		} else {
			// Yellow, not red: nothing is wrong, the next command renews it.
			expired()
			color.New(color.FgYellow).Fprintln(w, "  Status:    Access token expired; it refreshes automatically on the next command")
		}
		if a.RefreshAvailable {
			fmt.Fprintln(w, "  Refresh:   Available")
		}
		footer()

	case models.SessionExpired:
		color.New(color.FgRed).Fprintln(w, "✗ Session expired")
		if a.Email != "" {
			fmt.Fprintf(w, "  Email:     %s\n", a.Email)
		}
		expired()
		footer()
		fmt.Fprintln(w, "\nRun 'ndcli auth login' to authenticate")

	default:
		color.New(color.FgRed).Fprintln(w, "✗ Not authenticated")
		footer()
		fmt.Fprintln(w, "\nRun 'ndcli auth login' to authenticate")
	}
}

// AgoSince renders how long before now t was. A token inside the 60 s refresh
// buffer has not reached its expiry yet and reads as "just now".
func AgoSince(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func expiresAt(a *models.AuthShow) time.Time {
	if a.ExpiresAt == nil {
		return time.Time{}
	}
	return *a.ExpiresAt
}

// WriteConfigView is the human view of `config show`.
func WriteConfigView(w io.Writer, c *models.ConfigView) {
	heading := color.New(color.FgCyan)
	_, _ = heading.Fprintln(w, "Configuration:")
	fmt.Fprintf(w, "  Config file:  %s\n", c.ConfigFile)
	fmt.Fprintf(w, "  Auth storage: %s\n", c.Auth.Storage)
	if c.Auth.File != "" {
		fmt.Fprintf(w, "  Auth file:    %s\n", c.Auth.File)
	}
	if c.Auth.Account != "" {
		fmt.Fprintf(w, "  Auth account: %s\n", c.Auth.Account)
	}
	fmt.Fprintln(w)

	_, _ = heading.Fprintln(w, "Controlplane:")
	fmt.Fprintf(w, "  Host:       %s\n", c.Controlplane.Host)
	fmt.Fprintf(w, "  SSL Verify: %v\n", c.Controlplane.SSLVerify)
	fmt.Fprintln(w)

	_, _ = heading.Fprintln(w, "Connection relay (pathfinder):")
	if c.Pathfinder.Host != "" {
		fmt.Fprintf(w, "  Host:       %s\n", c.Pathfinder.Host)
		fmt.Fprintf(w, "  SSL Verify: %v\n", c.Pathfinder.SSLVerify)
	} else {
		fmt.Fprintln(w, "  Host:       (not configured)")
	}
	fmt.Fprintln(w)

	_, _ = heading.Fprintln(w, "Organization:")
	if c.Organization.Name != "" {
		fmt.Fprintf(w, "  Name: %s\n", c.Organization.Name)
	} else {
		fmt.Fprintln(w, "  Name: (not set)")
	}
	fmt.Fprintln(w)

	_, _ = heading.Fprintln(w, "Output:")
	fmt.Fprintf(w, "  Format:   %s\n", c.Output.Format)
	fmt.Fprintf(w, "  Timezone: %s\n", c.Output.Timezone)
}

// WriteVersion is the human view of `version`, two lines scripts read.
func WriteVersion(w io.Writer, v *models.VersionInfo) {
	fmt.Fprintf(w, "ndcli version %s\n", v.Version)
	fmt.Fprintf(w, "Build time: %s\n", v.BuildTime)
}

func (f *TableFormatter) FormatAuthShow(a *models.AuthShow) error {
	WriteAuthShow(f.Writer, a, time.Now())
	return nil
}
func (f *SimpleFormatter) FormatAuthShow(a *models.AuthShow) error {
	WriteAuthShow(f.Writer, a, time.Now())
	return nil
}
func (f *DetailedFormatter) FormatAuthShow(a *models.AuthShow) error {
	WriteAuthShow(f.Writer, a, time.Now())
	return nil
}
func (f *JSONFormatter) FormatAuthShow(a *models.AuthShow) error { return f.output(a) }

func (f *TableFormatter) FormatConfigView(c *models.ConfigView) error {
	WriteConfigView(f.Writer, c)
	return nil
}
func (f *SimpleFormatter) FormatConfigView(c *models.ConfigView) error {
	WriteConfigView(f.Writer, c)
	return nil
}
func (f *DetailedFormatter) FormatConfigView(c *models.ConfigView) error {
	WriteConfigView(f.Writer, c)
	return nil
}
func (f *JSONFormatter) FormatConfigView(c *models.ConfigView) error { return f.output(c) }

func (f *TableFormatter) FormatVersion(v *models.VersionInfo) error {
	WriteVersion(f.Writer, v)
	return nil
}
func (f *SimpleFormatter) FormatVersion(v *models.VersionInfo) error {
	WriteVersion(f.Writer, v)
	return nil
}
func (f *DetailedFormatter) FormatVersion(v *models.VersionInfo) error {
	WriteVersion(f.Writer, v)
	return nil
}
func (f *JSONFormatter) FormatVersion(v *models.VersionInfo) error { return f.output(v) }

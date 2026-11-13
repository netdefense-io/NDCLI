package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/mcp"
)

const usage = `netdefense-mcp is the NetDefense MCP server. It speaks MCP on stdin and
stdout, so an MCP client starts it; run by hand, it waits for a client on stdin
until stdin closes.

Usage:
  netdefense-mcp [--conf <file>]

Flags:
  --conf <file>   config file to use, as ndcli's --conf; it must exist, and
                  an absolute path is safest, since the MCP client chooses
                  the working directory (default: ndcli's default config,
                  whose server is production unless it says otherwise)
  -h, --help      print this help and exit
  --version       print the version and exit

Authentication is ndcli's: the session of 'ndcli auth login' for that config,
or a personal access token in NDCLI_TOKEN.
`

// options is what the command line asked for.
type options struct {
	configPath string
}

// errExit means the arguments were answered (help, version) and the server
// must not start.
var errExit = errors.New("exit")

// parseArgs reads the command line. Help and the version are written to out
// and return errExit; anything it does not know is an error. The server
// starts only when parseArgs returns no error.
func parseArgs(args []string, out io.Writer) (options, error) {
	var opts options
	fs := flag.NewFlagSet("netdefense-mcp", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.configPath, "conf", "", "")
	version := fs.Bool("version", false, "")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprint(out, usage)
			return opts, errExit
		}
		return opts, err
	}
	if *version {
		_, _ = fmt.Fprintf(out, "netdefense-mcp version %s\n", config.Version)
		return opts, errExit
	}
	if fs.NArg() > 0 {
		return opts, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	// ndcli creates a missing config file with the defaults, whose server is
	// production, and an empty path is the default config: a mistyped path or
	// an unexpanded variable must not start an unattended server there.
	confGiven := false
	fs.Visit(func(f *flag.Flag) { confGiven = confGiven || f.Name == "conf" })
	if confGiven {
		if opts.configPath == "" {
			return opts, errors.New("--conf needs a file")
		}
		fi, err := os.Stat(opts.configPath)
		if err != nil {
			return opts, fmt.Errorf("config file: %w", err)
		}
		if !fi.Mode().IsRegular() {
			return opts, fmt.Errorf("config file %s is not a regular file", opts.configPath)
		}
	}
	return opts, nil
}

func main() {
	opts, err := parseArgs(os.Args[1:], os.Stdout)
	if errors.Is(err, errExit) {
		return
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "netdefense-mcp: %v\n\n%s", err, usage)
		os.Exit(2)
	}

	server, err := mcp.NewServer(opts.configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize MCP server: %v\n", err)
		os.Exit(1)
	}

	// Start serving on stdio
	if err := server.Serve(); err != nil {
		fmt.Fprintf(os.Stderr, "MCP server error: %v\n", err)
		os.Exit(1)
	}
}

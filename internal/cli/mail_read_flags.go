package cli

import (
	"flag"
	"fmt"
	"strings"
)

// parseReadFlags permits options between positional arguments. Flag definitions
// determine which options consume a value, so new filters need no parser hooks.
// A standalone -- makes all remaining arguments literal.
func parseReadFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	var options, positionals []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			positionals = append(positionals, arg)
			continue
		}
		name, _, inline := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(arg, "-"), "-"), "=")
		f := fs.Lookup(name)
		if f == nil {
			if name == "h" || name == "help" {
				// Preserve flag.FlagSet's built-in help behavior.
				options = append(options, arg)
				continue
			}
			return nil, fmt.Errorf("unknown option %s for %s", arg, fs.Name())
		}
		options = append(options, arg)
		boolean, ok := f.Value.(interface{ IsBoolFlag() bool })
		if !inline && !(ok && boolean.IsBoolFlag()) {
			if i+1 == len(args) {
				return nil, fmt.Errorf("option %s requires a value", arg)
			}
			i++
			options = append(options, args[i])
		}
	}
	if err := fs.Parse(options); err != nil {
		return nil, err
	}
	return positionals, nil
}

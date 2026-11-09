// Command rainward follows a storm across a lot and plans where to catch the water.
//
//	rainward lot.json                         a plain-text report
//	rainward -storm twenty-year lot.json      plan for a different storm
//	rainward -budget 5000 lot.json            and a different budget (dollars)
//	rainward -json lot.json                   the full result as JSON
//
// Exit codes: 0 done, 2 bad usage or input.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/reihooneh/rainward/engine"
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("rainward", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, "")
	storm := fs.String("storm", "", "")
	budget := fs.Int("budget", 0, "")
	usage := "usage: rainward [-storm downpour|five-year|twenty-year] [-budget dollars] [-json] <lot.json>   (or - for standard input)"
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	if *budget < 0 || *budget > 50000 {
		fmt.Fprintln(stderr, "rainward: -budget is 0 to 50000 dollars")
		return 2
	}
	var r io.Reader = stdin
	if name := fs.Arg(0); name != "-" {
		info, err := os.Lstat(name)
		if err != nil || !info.Mode().IsRegular() {
			fmt.Fprintln(stderr, "rainward: the lot could not be read (it must be a regular file)")
			return 2
		}
		f, err := os.Open(name)
		if err != nil {
			fmt.Fprintln(stderr, "rainward: the lot could not be read (it must be a regular file)")
			return 2
		}
		defer f.Close()
		r = f
	}
	lot, err := engine.ParseLot(r)
	if err == nil {
		var res *engine.Result
		res, err = engine.Run(lot, engine.Options{Storm: *storm, Budget: *budget})
		if err == nil {
			if *asJSON {
				if err := json.NewEncoder(stdout).Encode(res); err != nil {
					fmt.Fprintln(stderr, "rainward: could not write the result")
					return 2
				}
				return 0
			}
			fmt.Fprint(stdout, report(res))
			return 0
		}
	}
	var in *engine.InputError
	if errors.As(err, &in) {
		fmt.Fprintf(stderr, "rainward: %s\n", in.Msg)
	} else {
		fmt.Fprintln(stderr, "rainward: the lot could not be planned")
	}
	return 2
}

func litres(m3 float64) string {
	v := int(m3*1000 + 0.5)
	s := fmt.Sprint(v)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s + " L"
}

func report(r *engine.Result) string {
	var b strings.Builder
	t, p := r.Today, r.Planned
	fmt.Fprintf(&b, "Rainward: %s\n%s, %s soil, budget $%.0f\n\n", r.Lot.Name, r.Storm.Describe, strings.ToLower(r.Soil.Name), r.Budget)
	row := func(label string, a, c float64) {
		fmt.Fprintf(&b, "  %-34s %12s %12s\n", label, litres(a), litres(c))
	}
	fmt.Fprintf(&b, "  %-34s %12s %12s\n", "", "today", "with plan")
	row("rain and roof water", t.Rain+t.Roof, p.Rain+p.Roof)
	row("caught in tanks", t.Tanks, p.Tanks)
	row("soaked in during the storm", t.Soaked, p.Soaked)
	row("left standing in hollows", t.Ponded, p.Ponded)
	row("ran to the street", t.Street, p.Street)
	row("ran onto neighbours' land", t.Neighbours, p.Neighbours)
	row("ran along the house walls", t.WallFlow, p.WallFlow)
	row("pooled against the house", t.WallPond, p.WallPond)
	row("pooled for more than a day", t.LongPond, p.LongPond)
	fmt.Fprintf(&b, "\n  score (weighted litres, lower is better): %.0f → %.0f (%.0f%% less)\n\n", t.Damage, p.Damage, 100-100*p.Damage/t.Damage)
	if len(r.Actions) == 0 {
		b.WriteString("Nothing within the budget helps enough to be worth doing.\n")
		return b.String()
	}
	fmt.Fprintf(&b, "The plan ($%.0f), best value first:\n", r.Cost)
	for i, a := range r.Actions {
		fmt.Fprintf(&b, "  %d. %-60s $%5.0f   score %6.0f\n", i+1, a.Text, a.Cost, a.Damage)
	}
	return b.String()
}

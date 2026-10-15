// Command terra-conformance checks that a program honours the module-side HTTP
// contract of a Terra host. It launches the program with a freshly issued
// identity, plays the host, and prints one PASS/FAIL line per rule.
//
//	terra-conformance [-module-id id] [-operation /path] [-bind-timeout 20s] [-launches 2] [-json] [-svi] [-core-invoke] -- command [args...]
//
// With -json, stdout carries only one JSON document (conformance.JSONReport)
// and the human lines are not printed.
//
// Exit status: 0 all rules passed (warnings and skips do not fail), 1 a rule
// failed, 2 usage or harness error.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/StellaxiaLab/terra-sdk/conformance"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	flags := flag.NewFlagSet("terra-conformance", flag.ContinueOnError)
	moduleID := flags.String("module-id", "", "module id to issue (default io.terra.conformance.sample)")
	operation := flags.String("operation", "", "one of the module's own routes to check for the credential guard (optional)")
	bind := flags.Duration("bind-timeout", 20*time.Second, "how long the module has to start listening")
	launches := flags.Int("launches", 2, "how many times to launch the module, each with a fresh identity")
	asJSON := flags.Bool("json", false, "print one JSON report on stdout instead of PASS/FAIL lines")
	svi := flags.Bool("svi", false, "also check the optional /terra/svi/* paths (module declares contributions.svi)")
	coreInvoke := flags.Bool("core-invoke", false, "record the /terra/core/invoke rule (host-served; reported as skipped)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	rest := flags.Args()
	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, "usage: terra-conformance [flags] -- command [args...]")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	report, err := conformance.Run(ctx, conformance.Spec{
		Command:       rest[0],
		Args:          rest[1:],
		ModuleID:      *moduleID,
		OperationPath: *operation,
		BindTimeout:   *bind,
		Launches:      *launches,
		SVI:           *svi,
		CoreInvoke:    *coreInvoke,
	})
	if *asJSON {
		out, _ := json.MarshalIndent(report.ToJSON(err), "", "  ")
		fmt.Println(string(out))
	} else {
		fmt.Print(report.String())
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "terra-conformance:", err)
		return 2
	}
	if !report.OK() {
		return 1
	}
	return 0
}

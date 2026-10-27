package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json/jsontext"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"os/user"
	"syscall"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/jobsclient"
)

const jobsSubmitUsage = "usage: linuxio-bridge jobs submit --route ROUTE [--request JSON] [--wait] | --resume ID"

// runJobsSubmit starts a job in the caller's own job host, as a bridge does,
// or resumes an interrupted one. Root starts the host's socket; a user's
// socket exists once they have logged in to LinuxIO since boot. It exits 2 on
// bad arguments, and with --wait 0 or 1 on the job's outcome.
func runJobsSubmit(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("jobs submit", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	route := flags.String("route", "", "")
	request := flags.String("request", "{}", "")
	wait := flags.Bool("wait", false, "")
	resume := flags.String("resume", "", "")
	if err := flags.Parse(args); err != nil || flags.NArg() > 0 || (*route == "") == (*resume == "") ||
		!jsontext.Value(*request).IsValid() {
		fmt.Fprintln(stderr, jobsSubmitUsage)
		return 2
	}

	uid := uint32(os.Getuid())
	account, err := user.Current()
	if err != nil {
		fmt.Fprintf(stderr, "linuxio-bridge jobs submit: look up the current user: %v\n", err)
		return 1
	}
	var ref [4]byte
	_, _ = rand.Read(ref[:])
	hello := bridgeipc.HelloSession{Username: account.Username, UID: uid, Privileged: uid == 0, SessionRef: "cli-" + hex.EncodeToString(ref[:])}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	c, err := jobsclient.Connect(ctx, uid, hello, jobsclient.HostSocketPath(uid))
	if err != nil {
		fmt.Fprintf(stderr, "linuxio-bridge jobs submit: %v\n", err)
		return 1
	}
	defer func() { _ = c.Close() }()

	if *resume != "" {
		if err = jobsclient.Resume(ctx, c, *resume); err != nil {
			fmt.Fprintf(stderr, "linuxio-bridge jobs submit: resume %s: %v\n", *resume, err)
			return 1
		}
		return 0
	}
	code, err := jobsclient.Submit(ctx, c, *route, jsontext.Value(*request), *wait, stdout)
	if err != nil {
		fmt.Fprintf(stderr, "linuxio-bridge jobs submit: %v\n", err)
		return 1
	}
	return code
}

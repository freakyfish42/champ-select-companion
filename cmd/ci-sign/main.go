// ci-sign signs a release inside GitHub Actions (the Release workflow). The private key comes from the
// repository secret SIGNING_KEY: the same one-line key as %APPDATA%\companion-sign\signing.key on the owner's PC.
//
//	go run ./cmd/ci-sign dist/champ-select-companion.exe v1.2.3 [latest-released-tag]
//
// It refuses to sign unless the exe carries exactly that version, the version is newer than the latest
// release, and the signature checks out against the update key built into the app (updatekey.go), so a
// wrong secret can never ship an update the team's apps would reject.
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/freakyfish42/champ-select-companion/core"
)

func fail(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "::error::"+f+"\n", a...)
	os.Exit(1)
}

func main() {
	if len(os.Args) < 3 {
		fail("usage: ci-sign <exe> <version> [latest]")
	}
	exe, want := os.Args[1], os.Args[2]
	if len(os.Args) > 3 && os.Args[3] != "" && !core.Newer(os.Args[3], want) {
		fail("%s isn't newer than the latest release %s", want, os.Args[3])
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("SIGNING_KEY")))
	if err != nil || len(seed) != ed25519.SeedSize {
		fail("the SIGNING_KEY secret is missing or isn't the one-line key from signing.key")
	}
	bin, err := os.ReadFile(exe)
	if err != nil {
		fail("%v", err)
	}
	tag, sig, err := core.SignRelease(ed25519.NewKeyFromSeed(seed), bin)
	if err != nil {
		fail("%v", err)
	}
	if tag != want {
		fail("the exe says it is %s, not %s", tag, want)
	}
	src, _ := os.ReadFile("updatekey.go")
	m := regexp.MustCompile(`updateKey = "([^"]+)"`).FindSubmatch(src)
	if m == nil {
		fail("no update key in updatekey.go")
	}
	if err := core.VerifyRelease(string(m[1]), tag, bin, sig); err != nil {
		fail("the SIGNING_KEY secret doesn't match the app's update key: %v", err)
	}
	if err := os.WriteFile(exe+".sig", sig, 0o644); err != nil {
		fail("%v", err)
	}
	fmt.Printf("signed %s (%s)\n", exe, tag)
}

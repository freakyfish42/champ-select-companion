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
	raw := strings.TrimSpace(os.Getenv("SIGNING_KEY"))
	src, _ := os.ReadFile("updatekey.go")
	m := regexp.MustCompile(`updateKey = "([^"]+)"`).FindSubmatch(src)
	if m == nil {
		fail("no update key in updatekey.go")
	}
	pub := string(m[1])
	switch {
	case raw == "":
		fail("the SIGNING_KEY secret is empty or missing (repo Settings → Secrets and variables → Actions)")
	case strings.TrimSpace(raw) == pub:
		fail("SIGNING_KEY holds the PUBLIC key (%s…), the one companion-sign.exe shows. It must be the line in %%APPDATA%%\\companion-sign\\signing.key", pub[:6])
	}
	// the key line is 44 characters ending in "="; extra text around it (a label, a second line) is ignored.
	// Whatever is picked is still checked against the app's update key below, so a wrong pick can't ship.
	var seed []byte
	found := 0
	for _, tok := range regexp.MustCompile(`[A-Za-z0-9+/]{43}=`).FindAllString(raw, -1) {
		if tok == pub {
			continue
		}
		if b, err := base64.StdEncoding.DecodeString(tok); err == nil && len(b) == ed25519.SeedSize {
			seed, found = b, found+1
		}
	}
	switch {
	case found == 0:
		fail("SIGNING_KEY has no key line in it (%d characters; a key line is 44, ending in =): paste only the line from signing.key", len(raw))
	case found > 1:
		fail("SIGNING_KEY has %d key-like lines in it: paste only the one line from signing.key", found)
	case len(raw) != 44:
		fmt.Println("::notice::SIGNING_KEY has extra text around the key line; it was ignored")
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
	if err := core.VerifyRelease(pub, tag, bin, sig); err != nil {
		fail("the SIGNING_KEY secret doesn't match the app's update key: %v", err)
	}
	if err := os.WriteFile(exe+".sig", sig, 0o644); err != nil {
		fail("%v", err)
	}
	fmt.Printf("signed %s (%s)\n", exe, tag)
}

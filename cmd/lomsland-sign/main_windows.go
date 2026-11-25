// lomsland-sign signs lømsland companion releases with a key that stays on the owner's PC.
//
//	Double-click:            creates the key the first time and shows its public part.
//	Drop an exe onto it:     writes <exe>.sig next to it. Upload both to the GitHub release.
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/freakyfish42/lomsland-companion/core"
	"golang.org/x/sys/windows"
)

var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	msgBox = user32.NewProc("MessageBoxW")
)

func say(text string, warn bool) {
	icon := uintptr(0x40)
	if warn {
		icon = 0x30
	}
	t, _ := windows.UTF16PtrFromString(text)
	c, _ := windows.UTF16PtrFromString("lømsland release signing")
	msgBox.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), icon)
}

func keyPath() string {
	base := os.Getenv("APPDATA")
	if base == "" {
		base, _ = os.UserConfigDir()
	}
	return filepath.Join(base, "lomsland-sign", "signing.key")
}

func loadKey() (ed25519.PrivateKey, error) {
	b, err := os.ReadFile(keyPath())
	if err != nil {
		return nil, err
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, os.ErrInvalid
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

func main() {
	key, err := loadKey()
	if len(os.Args) < 2 {
		created := false
		if os.IsNotExist(err) {
			_, priv, gerr := ed25519.GenerateKey(nil)
			if gerr == nil {
				_ = os.MkdirAll(filepath.Dir(keyPath()), 0o700)
				gerr = os.WriteFile(keyPath(), []byte(base64.StdEncoding.EncodeToString(priv.Seed())+"\n"), 0o600)
			}
			if gerr != nil {
				say("Couldn't create the signing key: "+gerr.Error(), true)
				return
			}
			key, err, created = priv, nil, true
		}
		if err != nil {
			say("The signing key at "+keyPath()+" is damaged: "+err.Error(), true)
			return
		}
		pub := base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey))
		copied := "Press Ctrl+C now to copy this whole message, then paste it to Claude.\n\n"
		head := "Your update key (the public part, safe to share):"
		if created {
			head = "Created your signing key. " + head
		}
		say(head+"\n\n"+pub+"\n\n"+copied+
			"The private part is in\n"+keyPath()+"\nIt never leaves this PC. Keep a copy on a USB stick: without it you can't sign updates, "+
			"and everyone would have to reinstall by hand.\n\nTo sign a release, drag lomsland-companion.exe onto lomsland-sign.exe.", false)
		return
	}
	if err != nil {
		say("No signing key on this PC yet. Double-click lomsland-sign.exe first.", true)
		return
	}
	done := []string{}
	for _, f := range os.Args[1:] {
		bin, err := os.ReadFile(f)
		if err == nil {
			var tag string
			var sig []byte
			if tag, sig, err = core.SignRelease(key, bin); err == nil {
				if err = os.WriteFile(f+".sig", sig, 0o644); err == nil {
					done = append(done, filepath.Base(f)+" ("+tag+") → "+filepath.Base(f)+".sig")
					continue
				}
			}
		}
		say("Couldn't sign "+filepath.Base(f)+": "+err.Error(), true)
	}
	if len(done) > 0 {
		say("Signed:\n"+strings.Join(done, "\n")+"\n\nUpload both files to the GitHub release with that version as its tag.", false)
	}
}

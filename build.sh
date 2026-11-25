#!/bin/sh
# Builds dist/champ-select-companion.exe and dist/companion-sign.exe (Windows x64) from Linux or Windows with Go 1.22+.
# Usage: ./build.sh v1.0.0      Needs llvm-windres (or x86_64-w64-mingw32-windres) for the icon and manifest.
set -e
V=${1:-dev}
N=$(echo "$V" | sed 's/^v//; s/-.*//'); case "$N" in *.*.*) ;; *) N=0.0.0;; esac
C=$(echo "$N" | tr . ,)
cd "$(dirname "$0")"
# the owner's requirements (VISION.md) and all other tests must pass before anything is built
go test ./core >/dev/null || { echo "Tests failed: not building. Run: go test ./core"; exit 1; }
cat > app.rc <<RC
1 ICON "icon.ico"
1 24 "app.manifest"
1 VERSIONINFO
FILEVERSION $C,0
PRODUCTVERSION $C,0
BEGIN
  BLOCK "StringFileInfo"
  BEGIN
    BLOCK "040904B0"
    BEGIN
      VALUE "FileDescription", "Champ Select Companion"
      VALUE "FileVersion", "$N"
      VALUE "ProductName", "Champ Select Companion"
      VALUE "ProductVersion", "$N"
      VALUE "OriginalFilename", "champ-select-companion.exe"
    END
  END
  BLOCK "VarFileInfo"
  BEGIN
    VALUE "Translation", 0x409, 1200
  END
END
RC
WR=$(command -v llvm-windres || command -v x86_64-w64-mingw32-windres || ls /usr/lib/llvm*/bin/llvm-windres 2>/dev/null | tail -1)
$WR --target=pe-x86-64 -i app.rc -o rsrc_windows_amd64.syso 2>/dev/null || $WR -i app.rc -o rsrc_windows_amd64.syso
rm -f app.rc
mkdir -p dist
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H windowsgui -X main.version=$V -X main.versionMarker=LMVER:$V:LMVER" -o dist/champ-select-companion.exe .
rm -f dist/*.sha256
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H windowsgui" -o dist/companion-sign.exe ./cmd/companion-sign
ls -la dist

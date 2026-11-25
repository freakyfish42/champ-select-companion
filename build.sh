#!/bin/sh
# Builds dist/lomsland-companion.exe (Windows x64) from Linux or Windows with Go 1.22+.
# Usage: ./build.sh v1.0.0      Needs llvm-windres (or x86_64-w64-mingw32-windres) for the icon and manifest.
set -e
V=${1:-dev}
N=$(echo "$V" | sed 's/^v//; s/-.*//'); case "$N" in *.*.*) ;; *) N=0.0.0;; esac
C=$(echo "$N" | tr . ,)
cd "$(dirname "$0")"
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
      VALUE "CompanyName", "build.lomsland.no"
      VALUE "FileDescription", "lomsland companion"
      VALUE "FileVersion", "$N"
      VALUE "ProductName", "lomsland companion"
      VALUE "ProductVersion", "$N"
      VALUE "OriginalFilename", "lomsland-companion.exe"
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
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H windowsgui -X main.version=$V" -o dist/lomsland-companion.exe .
( cd dist && sha256sum lomsland-companion.exe > lomsland-companion.exe.sha256 )
ls -la dist

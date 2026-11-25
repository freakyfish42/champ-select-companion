package main

// updateKey is the owner's Ed25519 public key (base64). Updates are installed only when their .sig verifies against it.
// The private key never leaves the owner's PC (companion-sign.exe keeps it there). Empty means: no automatic updates.
var updateKey = "E8axCgNF9CADyfiVIguf9apeaPF+k7FV3mWAirN1Lww="

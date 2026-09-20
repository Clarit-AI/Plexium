// Package main contains a tiny SHA-256 helper used by the KHA-287 probe.
// Kept here so the probe main.go stays compact and so the hashing path is
// obviously the same shape every test fixture uses.
package main

import (
	"crypto/sha256"
	"encoding/hex"
)

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

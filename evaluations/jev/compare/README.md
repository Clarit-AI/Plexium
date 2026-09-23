# Offline three-arm comparison sidecar

This package binds one deterministic-baseline report and the Jev and Nano sections of one pilot report to the frozen tuning corpus and request inventory. `Build` validates the inventory against the fixture manifest, checks each report's source, protocol version, and fixture count, and records both the containing-file SHA-256 and the canonical selected-report SHA-256. `Verify` rebuilds that identity and fails closed on any mismatch.

The sidecar is created only after reports exist and is used only when gold is joined for scoring. It is not consumed by the live runner and contains no request credentials.

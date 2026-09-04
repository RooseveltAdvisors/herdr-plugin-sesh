# Vision

This repository is the RooseveltAdvisors house fork of fullerzz/herdr-plugin-sesh, a Sesh-inspired workspace picker and session manager for Herdr.
It exists because the captain's work runs inside Herdr, and Sesh-style navigation - one searchable picker over running workspaces, configured sessions, and zoxide history, with strict two-target toggles between recent targets - has to be day-one muscle memory there.
The fork is not a divergence of purpose: it is the same plugin, maintained at house cadence, under a house-owned identity, with the fixes the fleet needs when it needs them.
It is maintained for the captain's fleet; a public install stays safe because the fork-native identity keeps it from colliding with an upstream install.

## Why the fork exists

Two things cannot be done from a patch held upstream: owning the installed identity, and shipping on the house cadence.
The fork installs as RooseveltAdvisors.herdr-sesh, so its actions, keybindings, and state never collide with an upstream fullerzz.sesh install, and its docs can say exactly which bindings work here.
Fixes the fleet depends on - plugin state scoped per Herdr session, focus recorded from live plugin event payloads instead of ambient environment variables, and fallback when Herdr hands the plugin a stale binary path - land here when they are ready, validated by the house pipeline.
Project memory lives here too, so the next agent inherits the regressions and the reasoning, not just the code.

## What it must never diverge on

The Sesh-style TOML contract and its test fixtures stay compatible, because existing session files must keep working.
The Go module path stays github.com/fullerzz/herdr-plugin-sesh, so upstream merges stay mechanical instead of becoming rewrites.
The MIT license and the credit line to fullerzz/herdr-plugin-sesh stay in the manifest.
Upstream work lands only as merge commits through reviewed sync pull requests, never squashed or rebased, so every upstream change stays attributable and every house delta stays visible as a delta.
Releases keep the upstream discipline: a v-tag that matches the version in herdr-plugin.toml, cut only from a clean, green tree.

## The experience it must create

The picker opens instantly and Enter lands on the right workspace every time, because navigation is only muscle memory if it never costs a thought.
The `last` and `last-agent` toggles behave identically whether the previous focus came from the picker, a hook, or Herdr itself, and a closed target refuses cleanly instead of falling back to older history.
Install and update stay one command with one preview, and a release never asks a human to trust an unvalidated tree.
Nothing surprises the captain: every divergence from upstream is documented in this repository, not discovered at the keyboard.

## Non-goals

- Not a rewrite or redesign of upstream's architecture.
- Not a compatibility shim for the old fullerzz.sesh action ids.
- Not a fleet configuration store: no credentials, secrets, or personal data live here.
- Not a channel to upstream; contributions there are the captain's call, not the fork's habit.

## Done well, one year out

Upstream releases merge in within days of publication, and the delta stays a handful of files a reviewer can read in one sitting.
Every fix the fleet needed has either landed upstream or remains a small, tested, documented delta here.
A fresh machine reaches a working, pinned install with one command, and the picker behaves identically on it.
The changelog, the tests, and this file still tell the truth about what the fork adds and what it refuses to change.

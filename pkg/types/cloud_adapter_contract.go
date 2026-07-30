// Copyright 2025 Sylos contributors
// SPDX-License-Identifier: MIT License

package types

// ClassifierFunc maps provider-specific errors into the shared 3-bucket model.
// Each cloud or local adapter implements ClassifyXxxError and passes it to
// credentials.DoWithClassifiedRetry.
type ClassifierFunc func(err error) FSErrorClassification

// CloudAdapterChecklist summarizes what a new FS adapter must wire before ME autoscaler
// and retry paths treat it as production-ready. See docs/fs_error_classification.md
// in Migration-Engine for the full checklist table.
//
//  1. Implement ClassifyXxxError (fatal / throttle / ambiguous buckets).
//  2. Wrap every FSAdapter I/O path with DoWithClassifiedRetry (or withClassifiedRetry helper).
//  3. Wire IsAuthFailure + Refresh in that middleware for any backend with tokens
//     (OAuth / Spectra auth). Refresh is opaque to Migration-Engine workers: the
//     FS call blocks until refresh+retry succeeds or returns a classified auth error.
//     LocalFS is the exception (no tokens / no Refresh).
//  4. Embed or hold FSDegradationState; implement FSDegradationReporter including
//     GetDegradationState() so ME can bridge RateLimitedUntil / TakeRecentHits
//     (AIMD FS_THROTTLE + Progress Monitor rate-limit badge). Snapshot-only
//     DegradationState()/RecordSignal is not enough.
//  5. When src/dst share a backend instance, use one FSDegradationState for the group.
//  6. Register ProviderID in Migration-Engine pkg/scaling/profile.go.
//  7. Honor explicit RetryAfter from Classify — never downgrade to generic backoff only.
//  8. Expose FSConcurrencyHint (ActiveWorkers) for ambiguous-error correlation.
//  9. Implement FSStorageInfo (GetStorageInfo) — return Available=false when the
//     provider has no portable free-space API (e.g. SFTP).
type CloudAdapterChecklist struct{}

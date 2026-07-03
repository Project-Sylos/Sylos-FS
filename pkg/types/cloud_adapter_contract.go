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
//  3. Embed or hold FSDegradationState; implement FSDegradationReporter.
//  4. When src/dst share a backend instance, use one FSDegradationState for the group.
//  5. Register ProviderID in Migration-Engine pkg/scaling/profile.go.
//  6. Honor explicit RetryAfter from Classify — never downgrade to generic backoff only.
//  7. Expose FSConcurrencyHint (ActiveWorkers) for ambiguous-error correlation.
type CloudAdapterChecklist struct{}

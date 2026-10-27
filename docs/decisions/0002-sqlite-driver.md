# ADR-0002: SQLite driver

- Status: proposed (verify in Foundation benchmarks)
- Date: 2026-10-27

## Context

The database profile requires per-connection PRAGMAs, one serialized write connection, a small read pool, and `BEGIN IMMEDIATE`. The driver choice affects build (CGO), image size and performance.

## Decision

Candidates: `modernc.org/sqlite` (pure Go, no CGO, simple static builds) and `mattn/go-sqlite3` (CGO, mature). Proposed default: `modernc.org/sqlite`, unless benchmarks on the seed dataset (10.000 items) show an unacceptable gap against the performance targets.

## Consequences

Pure Go keeps the Docker build and cross-compilation simple. Re-evaluate if p95 or RSS targets are missed. Driver sits behind `internal/db` only.

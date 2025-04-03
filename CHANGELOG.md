# Changelog
All notable changes to this library will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this library adheres to Rust's notion of
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).
The most recent changes are listed first.

## [Unreleased]

### Added

- Add debug logging to gRPC entry and exit points.

- Add smoke test

- lightwalletd node operators can export a donation address in the
  GetLightdInfo gRPC.

- Add the ability to not create and maintain a compact block cache.


### Changed

- The `RawTransaction` values returned from a call to `GetMempoolStream`
  now report a `Height` value of `0`, in order to be consistent with
  the results of calls to `GetTransaction`. See the documentation of
  `RawTransaction` in `walletrpc/service.proto` for more details on
  the semantics of this field.

### Fixed

- GetLatestBlock should report latest block hash in little-endian
  format, not big-endian.

- Support empty block range end in `getaddresstxids` calls.

- Filter out mined transactions in `refreshMempoolTxns`

- Uniformly return height 0 for mempool `RawTransaction` results.

- Reduce lightwalletd startup time.

- Parsing of `getrawtransaction` results is now platform-independent.
  Previously, values of `-1` returned for the transaction height would
  be converted to different `RawTransaction.Height` values depending
  upon whether `lightwalletd` was being run on a 32-bit or 64-bit 
  platform.

## [Prior Releases]

This changelog was not created until after the release of v0.4.17

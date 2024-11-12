# Changelog
All notable changes to this library will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this library adheres to Rust's notion of
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- The `RawTransaction` values returned from a call to `GetMempoolStream`
  now report a `Height` value of `0`, in order to be consistent with
  the results of calls to `GetTransaction`. See the documentation of
  `RawTransaction` in `walletrpc/service.proto` for more details on
  the semantics of this field.

### Fixed

- Parsing of `getrawtransaction` results is now platform-independent.
  Previously, values of `-1` returned for the transaction height would
  be converted to different `RawTransaction.Height` values depending
  upon whether `lightwalletd` was being run on a 32-bit or 64-bit 
  platform.

## [Prior Releases]

This changelog was not created until after the release of v0.4.17

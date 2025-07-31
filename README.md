# xatu-sidecar

Embeddable beacon chain event streaming library for Ethereum consensus clients.

## Features

- Stream gossipsub events (blocks, attestations, aggregate proofs, blob sidecars, data columns)
- FFI-compatible for integration with Rust, C++, and other language clients  
- Efficient batching and compression
- Multiple output targets (Xatu, custom HTTP endpoints)
- Minimal performance impact on host client

## Overview

A lightweight Go library for streaming Ethereum beacon chain events from consensus clients to external observability systems. 

- FFI-compatible shared library for embedding in Ethereum clients
- Batched event processing for efficient data transmission
- Support for all major beacon chain gossipsub events
- Compatible with Xatu data collection infrastructure
- Configurable outputs and batching parameters

## Usage

1. Import as shared library
2. Initialize with configuration
3. Send events via FFI
4. Library handles batching, compression, and delivery

## Building

```bash
make clean
make build
```

This will generate `libxatu.so` (Linux), `libxatu.dylib` (macOS), or `libxatu.dll` (Windows).

## Integration Examples

- [Lighthouse](https://github.com/ethpandaops/dimhouse)

## Configuration

The library accepts a JSON configuration with the following structure:

```json
{
  "log_level": "info",
  "xatu": {
    "name": "my-beacon-node",
    "outputs": [
      {
        "name": "mainnet",
        "type": "xatu",
        "config": {
          "address": "localhost:8080",
          "tls": false,
          "maxQueueSize": 100000,
          "batchTimeout": "5s",
          "workers": 1
        }
      }
    ],
    "ethereum": {
      "genesis_time": 1606824023,
      "seconds_per_slot": 12,
      "slots_per_epoch": 32,
      "network": {
        "name": "mainnet",
        "id": 1
      }
    },
    "client": {
      "name": "my-beacon-node",
      "version": "1.0.0"
    },
    "ntpServer": "time.google.com"
  }
}
```

## FFI Functions

### Init
Initialize the library with configuration
```c
int32_t Init(const char* config_json);
```

### SendEventBatch
Send a batch of events
```c
int32_t SendEventBatch(const char* events_json);
```

### Shutdown
Gracefully shutdown the library
```c
void Shutdown();
```

### GetStatus
Get current library status
```c
const char* GetStatus();
```

## Event Types

- `BEACON_BLOCK`: Propagated beacon blocks
- `ATTESTATION`: Individual attestations
- `AGGREGATE_AND_PROOF`: Aggregated attestations
- `BLOB_SIDECAR`: Blob data for EIP-4844
- `DATA_COLUMN_SIDECAR`: Data availability sampling columns

## Integration via Pre-built Binaries

For Rust projects (like Lighthouse), the recommended integration approach is:

1. **xatu-sidecar releases** will include pre-built shared libraries:
   - `libxatu.so` (Linux)
   - `libxatu.dylib` (macOS) 
   - `libxatu.dll` (Windows)

2. **Consumer's build.rs** downloads the appropriate binary:
   ```rust
   // In build.rs
   - Detect target platform
   - Download matching pre-built library from GitHub releases
   - Verify checksum
   - Place in expected location
   - Link against it
   ```

3. **Benefits**:
   - No Go toolchain required for users
   - Faster builds
   - Consistent binaries across all consumers
   - Version pinning via release tags

This approach is similar to how projects handle native dependencies like RocksDB or OpenSSL bindings.

## License

[Add appropriate license here]
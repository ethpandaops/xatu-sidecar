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
make
```

This will generate `libxatu.so` (Linux), `libxatu.dylib` (macOS), or `libxatu.dll` (Windows).

## Integration Examples

- [Lighthouse](https://github.com/ethpandaops/dimhouse)

## FFI Functions

### Init
Initialize the library with configuration

> **Note:** See [Configuration](#configuration) for the configuration format.

```c
int32_t Init(const char* config_yaml);
```

### SendEventBatch
Send a batch of events

> **Note:** See [FFI Event Payloads](#ffi-event-payloads) for the event format.

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

## Configuration

The library accepts a YAML configuration with the following structure:

```yaml
log_level: info
processor:
  name: my-beacon-node
  outputs:
    - name: xatu-server-1
      type: xatu
      config:
        address: localhost:8080
        tls: false
        maxQueueSize: 100000
        batchTimeout: 5s
        workers: 1
    - name: standard-out
      type: stdout
  ethereum:
    genesis_time: 1606824023
    seconds_per_slot: 12
    slots_per_epoch: 32
    network:
      name: mainnet
      id: 1
  client:
    name: my-beacon-node
    version: 1.0.0
  ntpServer: time.google.com
```

## Event Types

- `BEACON_BLOCK`: Propagated beacon blocks
- `ATTESTATION`: Individual attestations
- `AGGREGATE_AND_PROOF`: Aggregated attestations
- `BLOB_SIDECAR`: Blob data for EIP-4844
- `DATA_COLUMN_SIDECAR`: Data availability sampling columns (not yet implemented)

## FFI Event Payloads

When sending events via the `SendEventBatch` function, events must be sent as a JSON array. Each event in the array must include an `event_type` field matching one of the EventType constants, along with the event-specific fields.

### Event Type Values

The `event_type` field must be one of the following string values:
- `"BEACON_BLOCK"` - Propagated beacon blocks
- `"ATTESTATION"` - Individual attestations  
- `"AGGREGATE_AND_PROOF"` - Aggregated attestations
- `"BLOB_SIDECAR"` - Blob data for EIP-4844
- `"DATA_COLUMN_SIDECAR"` - Data availability sampling columns (not yet implemented)

### BEACON_BLOCK Payload
```json
{
    "event_type": "BEACON_BLOCK",
    "peer_id": "16Uiu2HAm...",
    "message_id": "msg_12345",
    "topic": "/eth2/abc123/beacon_block/ssz_snappy",
    "message_size": 1024,
    "timestamp_ms": 1234567890123,
    "slot": 1234567,
    "epoch": 38576,
    "block_root": "0x1234567890abcdef...",
    "proposer_index": 12345
}
```

### ATTESTATION Payload
```json
{
    "event_type": "ATTESTATION",
    "peer_id": "16Uiu2HAm...",
    "message_id": "msg_12345",
    "slot": 1234567,
    "epoch": 38576,
    "attestation_data_root": "0x1234567890abcdef...",
    "subnet_id": 0,
    "timestamp": 1234567890,
    "should_process": true,
    "topic": "/eth2/abc123/beacon_attestation_0/ssz_snappy",
    "message_size": 1024,
    "source_epoch": 38575,
    "source_root": "0xabcdef1234567890...",
    "target_epoch": 38576,
    "target_root": "0xfedcba0987654321...",
    "committee_index": 10,
    "aggregation_bits": "0x01",
    "signature": "0x987654321...",
    "attester_index": 12345
}
```

### AGGREGATE_AND_PROOF Payload
```json
{
    "event_type": "AGGREGATE_AND_PROOF",
    "peer_id": "16Uiu2HAm...",
    "message_id": "msg_12345",
    "slot": 1234567,
    "epoch": 38576,
    "attestation_data_root": "0x1234567890abcdef...",
    "aggregator_index": 12345,
    "timestamp": 1234567890,
    "topic": "/eth2/abc123/beacon_aggregate_and_proof/ssz_snappy",
    "message_size": 1024,
    "source_epoch": 38575,
    "source_root": "0xabcdef1234567890...",
    "target_epoch": 38576,
    "target_root": "0xfedcba0987654321...",
    "committee_index": 10,
    "aggregation_bits": "0x01ff",
    "signature": "0x987654321..."
}
```

### BLOB_SIDECAR Payload
```json
{
    "event_type": "BLOB_SIDECAR",
    "peer_id": "16Uiu2HAm...",
    "message_id": "msg_12345",
    "topic": "/eth2/abc123/blob_sidecar_0/ssz_snappy",
    "message_size": 131072,
    "timestamp_ms": 1234567890123,
    "slot": 1234567,
    "epoch": 38576,
    "block_root": "0x1234567890abcdef...",
    "parent_root": "0xabcdef1234567890...",
    "state_root": "0xfedcba0987654321...",
    "proposer_index": 12345,
    "blob_index": 0,
    "client": "lighthouse/v4.5.0"
}
```

### DATA_COLUMN_SIDECAR Payload

**TBD**

### Example Batch Request
```json
[
    {
        "event_type": "BEACON_BLOCK",
        "peer_id": "16Uiu2HAm...",
        "message_id": "msg_12345",
        "topic": "/eth2/abc123/beacon_block/ssz_snappy",
        "message_size": 1024,
        "timestamp_ms": 1234567890123,
        "slot": 1234567,
        "epoch": 38576,
        "block_root": "0x1234567890abcdef...",
        "proposer_index": 12345
    },
    {
        "event_type": "ATTESTATION",
        "peer_id": "16Uiu2HAm...",
        "message_id": "msg_12346",
        "slot": 1234567,
        "epoch": 38576,
        "attestation_data_root": "0x1234567890abcdef...",
        "subnet_id": 0,
        "timestamp": 1234567890,
        "should_process": true,
        "topic": "/eth2/abc123/beacon_attestation_0/ssz_snappy",
        "message_size": 512,
        "source_epoch": 38575,
        "source_root": "0xabcdef1234567890...",
        "target_epoch": 38576,
        "target_root": "0xfedcba0987654321...",
        "committee_index": 10,
        "aggregation_bits": "0x01",
        "signature": "0x987654321...",
        "attester_index": 12345
    }
]
```

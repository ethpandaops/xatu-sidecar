# xatu-sidecar

Embeddable beacon chain event streaming library for Ethereum consensus clients (Linux only).

## Features

- Stream gossipsub events (blocks, attestations, aggregate proofs, blob sidecars, data columns)
- FFI-compatible for integration with Rust, C++, and other language clients  
- Efficient batching and compression
- Multiple output targets (Xatu, custom HTTP endpoints)
- Minimal performance impact on host client

## Overview

A lightweight Go library for streaming Ethereum beacon chain events from consensus clients to external observability systems. 

- FFI-compatible shared library for embedding in Ethereum clients (Linux only)
- Batched event processing for efficient data transmission
- Support for all major beacon chain gossipsub events
- Compatible with Xatu data collection infrastructure
- Configurable outputs and batching parameters

## Usage

1. Import as shared library
2. Initialize with configuration
3. Send events via FFI/NFI
4. Library handles batching, compression, and delivery

## Building

```bash
make clean
make
```

This will generate `libxatu.so` for Linux (amd64 and arm64 architectures).

## Integration Examples

- [Lighthouse](https://github.com/ethpandaops/dimhouse)

## Functions

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
    implementation: lighthouse
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

## FFI Event Payloads

When sending events via the `SendEventBatch` function, events must be sent as a JSON array. Each event in the array must include an `event_type` field matching one of the EventType constants, along with the event-specific fields.

### Event Type Values

The `event_type` field must be one of the following string values:
- `"BEACON_BLOCK"` - Propagated beacon blocks
- `"ATTESTATION"` - Individual attestations  
- `"AGGREGATE_AND_PROOF"` - Aggregated attestations
- `"BLOB_SIDECAR"` - Blob data for EIP-4844
- `"DATA_COLUMN_SIDECAR"` - Data availability sampling columns
- `"PARTIAL_DATA_COLUMN"` - Gossipsub partial messages extension parts on data column topics
- `"DATA_COLUMN_COMPLETED"` - A custody data column became fully available, by whatever path
- `"PARTIAL_DATA_COLUMN_OUTCOME"` - Validation verdict for a received partial data column

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
    "timestamp_ms": 1234567890123,
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
    "timestamp_ms": 1234567890123,
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
```json
{
    "event_type": "DATA_COLUMN_SIDECAR",
    "peer_id": "16Uiu2HAm...",
    "message_id": "msg_12345",
    "topic": "/eth2/abc123/data_column_sidecar_0/ssz_snappy",
    "message_size": 131072,
    "timestamp_ms": 1234567890123,
    "slot": 1234567,
    "epoch": 38576,
    "block_root": "0x1234567890abcdef...",
    "parent_root": "0xabcdef1234567890...",
    "state_root": "0xfedcba0987654321...",
    "proposer_index": 12345,
    "column_index": 0,
    "kzg_commitments_count": 5,
    "client": "lighthouse/v4.5.0"
}
```

### PARTIAL_DATA_COLUMN Payload

One gossipsub partial messages extension part on a data column topic.

- `direction`: `RECV`, `SEND` or `DROP`. `capture_point`: `WIRE` (raw RPC) or `APP` (after the gossipsub router filtered it).
- `peer_id` is the sender for `RECV` and the recipient for `SEND`/`DROP`. It is empty when the client publishes to the router rather than to one peer.
- `group_id` is the hex partial group ID; `group_version` is its first byte (0 fulu, 1 gloas).
- `slot` is 0 when unknown (fulu groups only carry a block root). The sidecar then fills it from earlier events for the same `block_root`.
- `cells_bitmap`, `metadata_available` and `metadata_requests` are hex encoded SSZ bitlists. `bitmap_length` is the bitlist length (the block's blob count).
- `message_size` is the encoded partial message size, 0 for metadata-only parts.

```json
{
    "event_type": "PARTIAL_DATA_COLUMN",
    "timestamp_ms": 1234567890123,
    "direction": "RECV",
    "capture_point": "APP",
    "peer_id": "16Uiu2HAm...",
    "topic": "/eth2/abc123/data_column_sidecar_7/ssz_snappy",
    "group_id": "0x00abcdef...",
    "group_version": 0,
    "block_root": "0xabcdef...",
    "slot": 1234567,
    "column_index": 7,
    "has_message": true,
    "message_size": 4242,
    "cells_count": 2,
    "cells_bitmap": "0x0d",
    "bitmap_length": 3,
    "has_header": true,
    "kzg_commitments_count": 3,
    "proposer_index": 12345,
    "has_metadata": true,
    "metadata_size": 10,
    "metadata_available_count": 2,
    "metadata_requests_count": 3,
    "metadata_available": "0x0d",
    "metadata_requests": "0x0f"
}
```

### DATA_COLUMN_COMPLETED Payload

A custody data column became fully available. Only the first completion per `block_root` and `column_index` is exported.

- `completed_via`: `FULL_GOSSIP`, `PARTIAL_GOSSIP`, `EL`, `RECONSTRUCTION` or `UNKNOWN`.
- `peer_id` is the peer whose message completed the column, empty for `EL` and `RECONSTRUCTION`. `topic` is the column's `data_column_sidecar_{subnet}` topic, whatever the path.

```json
{
    "event_type": "DATA_COLUMN_COMPLETED",
    "timestamp_ms": 1234567890123,
    "peer_id": "16Uiu2HAm...",
    "topic": "/eth2/abc123/data_column_sidecar_7/ssz_snappy",
    "block_root": "0xabcdef...",
    "slot": 1234567,
    "column_index": 7,
    "completed_via": "PARTIAL_GOSSIP",
    "cells_total": 6,
    "cells_from_gossip": 2,
    "cells_from_el": 4,
    "contributing_peers": 1,
    "kzg_commitments_count": 6
}
```

### PARTIAL_DATA_COLUMN_OUTCOME Payload

The validation verdict for one received partial data column message.

- `stage`: `DECODE`, `HEADER` or `CELLS`. `outcome`: `ACCEPT`, `IGNORE` or `REJECT`. `reason` is empty for `ACCEPT`.
- `new_cells_count` is the number of cells the message added to the node's copy of the column.
- `slot` is 0 when unknown and is filled the same way as for `PARTIAL_DATA_COLUMN`.

```json
{
    "event_type": "PARTIAL_DATA_COLUMN_OUTCOME",
    "timestamp_ms": 1234567890123,
    "peer_id": "16Uiu2HAm...",
    "topic": "/eth2/abc123/data_column_sidecar_7/ssz_snappy",
    "group_id": "0x00abcdef...",
    "block_root": "0xabcdef...",
    "slot": 1234567,
    "column_index": 7,
    "stage": "CELLS",
    "outcome": "ACCEPT",
    "reason": "",
    "cells_count": 2,
    "new_cells_count": 1
}
```

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
        "timestamp_ms": 1234567890123,
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

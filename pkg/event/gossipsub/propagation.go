package gossipsub

import "time"

// slotStartDiffMs returns event time minus slot start in milliseconds, clamped at 0.
// For events whose propagation_slot_start_diff column is UInt32.
func slotStartDiffMs(eventTime, slotStart time.Time) uint64 {
	diff := eventTime.Sub(slotStart).Milliseconds()
	if diff < 0 {
		return 0
	}

	return uint64(diff)
}

// signedSlotStartDiffMs returns event time minus slot start in milliseconds as a two's
// complement uint64, for events gossiped before their slot starts (execution payload bids,
// proposer preferences) whose propagation_slot_start_diff column is Int32.
func signedSlotStartDiffMs(eventTime, slotStart time.Time) uint64 {
	return uint64(eventTime.Sub(slotStart).Milliseconds()) //nolint:gosec // intentional two's-complement encoding of a negative diff.
}

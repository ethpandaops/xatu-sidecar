// Package processor handles Ethereum blockchain event processing for the Xatu sidecar.
package processor

import (
	"errors"
	"fmt"

	"github.com/ethpandaops/xatu/pkg/output"
	"github.com/ethpandaops/xatu/pkg/processor"
	"github.com/sirupsen/logrus"
)

// Define static errors for validation.
var (
	ErrGenesisTimeRequired    = errors.New("genesis time is required")
	ErrSecondsPerSlotRequired = errors.New("seconds per slot is required")
	ErrSlotsPerEpochRequired  = errors.New("slots per epoch is required")
	ErrNetworkNameRequired    = errors.New("network name is required")
	ErrNetworkIDRequired      = errors.New("network id is required")
)

// Config represents the main configuration structure.
type Config struct {
	// The name of the processor
	Name string `yaml:"name"`

	// Outputs configuration
	Outputs []output.Config `yaml:"outputs"`

	// Ethereum
	Ethereum Ethereum `yaml:"ethereum"`

	// Client metadata
	Client Client `yaml:"client"`

	// NTP Server to use for clock drift correction
	NTPServer string `yaml:"ntpServer" default:"time.google.com"`
}

// Network represents Ethereum network configuration.
type Network struct {
	Name string `yaml:"name"`
	ID   uint64 `yaml:"id"`
}

// Ethereum contains Ethereum-specific configuration.
type Ethereum struct {
	GenesisTime    uint64  `yaml:"genesis_time"`
	SecondsPerSlot uint64  `yaml:"seconds_per_slot"`
	SlotsPerEpoch  uint64  `yaml:"slots_per_epoch"`
	Network        Network `yaml:"network"`
}

// Client represents the metadata for the client using the sidecar.
type Client struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	for _, output := range c.Outputs {
		if err := output.Validate(); err != nil {
			return fmt.Errorf("output %s: %w", output.Name, err)
		}
	}

	return nil
}

// Validate checks the Ethereum configuration for errors.
func (e *Ethereum) Validate() error {
	if e.GenesisTime == 0 {
		return ErrGenesisTimeRequired
	}

	if e.SecondsPerSlot == 0 {
		return ErrSecondsPerSlotRequired
	}

	if e.SlotsPerEpoch == 0 {
		return ErrSlotsPerEpochRequired
	}

	if err := e.Network.Validate(); err != nil {
		return err
	}

	return nil
}

// Validate checks the network configuration for errors.
func (n *Network) Validate() error {
	if n.Name == "" {
		return ErrNetworkNameRequired
	}

	if n.ID == 0 {
		return ErrNetworkIDRequired
	}

	return nil
}

// CreateSinks creates output sinks from the configuration.
func (c *Config) CreateSinks(log logrus.FieldLogger) ([]output.Sink, error) {
	sinks := make([]output.Sink, len(c.Outputs))

	for i, out := range c.Outputs {
		if out.ShippingMethod == nil {
			shippingMethod := processor.ShippingMethodAsync
			out.ShippingMethod = &shippingMethod
		}

		sink, err := output.NewSink(out.Name,
			out.SinkType,
			out.Config,
			log,
			out.FilterConfig,
			*out.ShippingMethod,
		)
		if err != nil {
			return nil, err
		}

		sinks[i] = sink
	}

	return sinks, nil
}

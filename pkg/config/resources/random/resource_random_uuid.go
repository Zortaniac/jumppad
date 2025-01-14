package random

import (
	"github.com/jumppad-labs/hclconfig/types"
	"github.com/jumppad-labs/jumppad/pkg/config"
)

// TypeRandomUUID is the resource for generating random UUIDs
const TypeRandomUUID string = "random_uuid"

// allows the generation of random UUIDs
type RandomUUID struct {
	types.ResourceMetadata `hcl:",remain"`

	// Output parameters
	Value string `hcl:"value,optional" json:"value"`
}

func (c *RandomUUID) Process() error {
	// do we have an existing resource in the state?
	// if so we need to set any computed resources for dependents
	cfg, err := config.LoadState()
	if err == nil {
		// try and find the resource in the state
		r, _ := cfg.FindResource(c.ID)
		if r != nil {
			state := r.(*RandomUUID)
			c.Value = state.Value
		}
	}

	return nil
}

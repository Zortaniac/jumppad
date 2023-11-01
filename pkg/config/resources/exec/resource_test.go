package exec

import (
	"os"
	"testing"

	"github.com/jumppad-labs/hclconfig/types"
	"github.com/jumppad-labs/jumppad/pkg/config"
	ctypes "github.com/jumppad-labs/jumppad/pkg/config/resources/container"
	"github.com/jumppad-labs/jumppad/testutils"
	"github.com/stretchr/testify/require"
)

func init() {
	config.RegisterResource(TypeExec, &Exec{}, &Provider{})
}

func TestExecSetsOutputsFromState(t *testing.T) {
	testutils.SetupState(t, `
{
  "blueprint": null,
  "resources": [
	{
			"id": "resource.exec.test",
      "name": "test",
      "status": "created",
      "type": "exec",
			"pid": 42
	}
	]
}`)

	c := &Exec{
		ResourceMetadata: types.ResourceMetadata{
			ID: "resource.exec.test",
		},
	}

	c.Process()

	require.Equal(t, 42, c.PID)
}

func TestExecProcessSetsAbsolute(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)

	c := &Exec{
		ResourceMetadata: types.ResourceMetadata{File: "./"},
		Image: &ctypes.Image{
			Name: "test",
		},
		Volumes: []ctypes.Volume{
			{
				Source:      "./",
				Destination: "./",
			},
		},
	}

	c.Process()

	require.Equal(t, wd, c.Volumes[0].Source)
}

func TestExecLocalWithVolumesReturnsError(t *testing.T) {
	c := &Exec{
		ResourceMetadata: types.ResourceMetadata{File: "./"},
		Volumes: []ctypes.Volume{
			{
				Source:      "./",
				Destination: "./",
			},
		},
	}

	err := c.Process()
	require.Error(t, err)
}

func TestExecLocalWithNetworksReturnsError(t *testing.T) {
	c := &Exec{
		ResourceMetadata: types.ResourceMetadata{File: "./"},
		Networks: []ctypes.NetworkAttachment{
			{
				Name: "test",
			},
		},
	}

	err := c.Process()
	require.Error(t, err)
}

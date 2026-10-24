package virt

import (
	"runtime"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func TestValidateCreateRequestRejectsInvalidInput(t *testing.T) {
	valid := apischema.VMCreateRequest{
		Name:     "good-vm",
		VCPUs:    2,
		MemoryMB: 1024,
		DiskGB:   20,
		ISOPath:  "/isos/test.iso",
	}
	tests := []struct {
		name string
		req  apischema.VMCreateRequest
	}{
		{name: "empty name", req: func() apischema.VMCreateRequest { r := valid; r.Name = ""; return r }()},
		{name: "path traversal name", req: func() apischema.VMCreateRequest { r := valid; r.Name = "bad..vm"; return r }()},
		{name: "zero vcpus", req: func() apischema.VMCreateRequest { r := valid; r.VCPUs = 0; return r }()},
		{name: "more vcpus than host", req: func() apischema.VMCreateRequest { r := valid; r.VCPUs = runtime.NumCPU() + 1; return r }()},
		{name: "too little memory", req: func() apischema.VMCreateRequest { r := valid; r.MemoryMB = 128; return r }()},
		{name: "zero disk", req: func() apischema.VMCreateRequest { r := valid; r.DiskGB = 0; return r }()},
		{name: "missing iso", req: func() apischema.VMCreateRequest { r := valid; r.ISOPath = ""; return r }()},
		{name: "malformed network", req: func() apischema.VMCreateRequest { r := valid; r.Network = "bad/name"; return r }()},
		{name: "image preset disk too small", req: apischema.VMCreateRequest{Name: "haos", VCPUs: 2, MemoryMB: 2048, DiskGB: 8, SourceType: vmSourceTypeImagePreset, ImagePresetID: vmImagePresetHomeOS}},
		{name: "cloud image without login", req: apischema.VMCreateRequest{Name: "debian", VCPUs: 2, MemoryMB: 2048, DiskGB: 12, SourceType: vmSourceTypeImagePreset, ImagePresetID: vmImagePresetDebian}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateCreateRequest(tt.req); err == nil {
				t.Fatal("validateCreateRequest succeeded, want error")
			}
		})
	}
}

func TestValidateCreateRequestReturnsBadRequestError(t *testing.T) {
	err := validateCreateRequest(apischema.VMCreateRequest{
		Name:     "good-vm",
		VCPUs:    0,
		MemoryMB: 1024,
		DiskGB:   20,
		ISOPath:  "/isos/test.iso",
	})

	if err == nil {
		t.Fatal("validateCreateRequest succeeded, want error")
	}
	if code := errorCode(err, 0); code != 400 {
		t.Fatalf("error code = %d, want 400", code)
	}
}

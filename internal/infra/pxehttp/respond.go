package pxehttp

import (
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/vm"
)

type errorResponse struct {
	Error string `json:"error"`
}

func jsonError(message string) errorResponse {
	return errorResponse{Error: message}
}

func jsonErrorErr(err error) errorResponse {
	return jsonError(err.Error())
}

type statusResponse struct {
	Status string `json:"status"`
}

type inventoryResponse struct {
	AttemptID       string                   `json:"attemptId"`
	DeployMode      string                   `json:"deployMode,omitempty"`
	CurtinConfigURL string                   `json:"curtinConfigUrl,omitempty"`
	EventsURL       string                   `json:"eventsUrl"`
	DiskImageDeploy *diskImageDeployResponse `json:"diskImageDeploy,omitempty"`
}

type diskImageDeployResponse struct {
	ImageURL            string                    `json:"imageUrl"`
	Format              string                    `json:"format"`
	OSFamily            string                    `json:"osFamily,omitempty"`
	OSVersion           string                    `json:"osVersion,omitempty"`
	TargetDisk          string                    `json:"targetDisk"`
	RootPartitionNumber int                       `json:"rootPartitionNumber"`
	EFIPartitionNumber  int                       `json:"efiPartitionNumber,omitempty"`
	SeedURL             string                    `json:"seedUrl"`
	SealedBootstrap     *diskImageSealedBootstrap `json:"sealedBootstrap,omitempty"`
}

type diskImageSealedBootstrap struct {
	Host           string `json:"host"`
	Owner          string `json:"owner"`
	KeyFingerprint string `json:"keyFingerprint"`
	Envelope       string `json:"envelope,omitempty"`
	Cleanup        bool   `json:"cleanup,omitempty"`
}

type installCompleteVMResponse struct {
	Status string            `json:"status"`
	VM     vm.VirtualMachine `json:"vm"`
}

type installCompleteMachineResponse struct {
	Status  string          `json:"status"`
	Machine machine.Machine `json:"machine"`
}

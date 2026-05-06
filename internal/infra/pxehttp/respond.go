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
	AttemptID       string `json:"attemptId"`
	CurtinConfigURL string `json:"curtinConfigUrl"`
	EventsURL       string `json:"eventsUrl"`
}

type installCompleteVMResponse struct {
	Status string            `json:"status"`
	VM     vm.VirtualMachine `json:"vm"`
}

type installCompleteMachineResponse struct {
	Status  string          `json:"status"`
	Machine machine.Machine `json:"machine"`
}

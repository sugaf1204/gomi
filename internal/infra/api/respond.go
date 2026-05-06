package api

import (
	"time"

	"github.com/sugaf1204/gomi/internal/auth"
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
	Status    string `json:"status"`
	RequestID string `json:"requestID,omitempty"`
}

type healthResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type tokenResponse struct {
	Token string `json:"token"`
}

type itemsResponse[T any] struct {
	Items []T `json:"items"`
}

type authUserResponse struct {
	Username string    `json:"username"`
	Role     auth.Role `json:"role"`
}

type loginResponse struct {
	Token   string           `json:"token"`
	Expires time.Time        `json:"expires"`
	User    authUserResponse `json:"user"`
}

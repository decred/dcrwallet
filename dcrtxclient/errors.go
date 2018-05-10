package dcrtxclient

import (
	"errors"
)

var (
	ErrAlreadyConnected = errors.New("Already connected to dcrtxmatcher server")
	ErrNotConnected     = errors.New("Client is not connected to dcrtxmatcher server")
	ErrCannotConnect    = errors.New("Unable to connect to dcrtxmatcher server")
)

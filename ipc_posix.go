// +build !windows

package main

import "net"

func serviceControlOpenNamedPipeRx(fname string) (net.Listener, error) {
	return net.Listen("unix", fname)
}

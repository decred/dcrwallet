package types

import (
	"net"

	"github.com/decred/dcrd/wire"
)

type GetSpvInfoResult struct {
	Id         		uint64 				`json:"id"`
	UA         		string  			`json:"useragent"`
	Services   		wire.ServiceFlag 	`json:"services"`
	Pver       		uint32				`json:"pver"`
	InitHeight 		int32  				`json:"initial height"`
	Raddr      		net.Addr			`json:"remote address"`
	NA         		*wire.NetAddress 	`json:"net address"`
	C       		net.Conn 			`json:"connection"`
	Sendheaders 	bool				`json:"sendheaders"`
}
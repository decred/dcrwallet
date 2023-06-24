package loader

import (
	"sync"

	"decred.org/dcrwallet/v4/errors"
	"decred.org/dcrwallet/v4/internal/loggers"

	vsp "github.com/decred/vspd/client/v2"
)

var vspClients = struct {
	mu      sync.Mutex
	clients map[string]*vsp.AutoClient
}{
	clients: make(map[string]*vsp.AutoClient),
}

// VSP loads or creates a package-global instance of the VSP client for a host.
// This allows clients to be created and reused across various subsystems.
func VSP(cfg vsp.Config) (*vsp.AutoClient, error) {
	key := cfg.URL
	vspClients.mu.Lock()
	defer vspClients.mu.Unlock()
	client, ok := vspClients.clients[key]
	if ok {
		return client, nil
	}
	client, err := vsp.New(cfg, loggers.VspcLog)
	if err != nil {
		return nil, err
	}
	vspClients.clients[key] = client
	return client, nil
}

// LookupVSP returns a previously-configured VSP client, if one has been created
// and registered with the VSP function.  Otherwise, a NotExist error is
// returned.
func LookupVSP(host string) (*vsp.AutoClient, error) {
	vspClients.mu.Lock()
	defer vspClients.mu.Unlock()
	client, ok := vspClients.clients[host]
	if !ok {
		err := errors.Errorf("VSP client for %q not found", host)
		return nil, errors.E(errors.NotExist, err)
	}
	return client, nil
}

// AllVSPs returns the list of all currently registered VSPs.
func AllVSPs() map[string]*vsp.AutoClient {
	// Create a copy to avoid callers mutating the list.
	vspClients.mu.Lock()
	defer vspClients.mu.Unlock()
	res := make(map[string]*vsp.AutoClient, len(vspClients.clients))
	for host, client := range vspClients.clients {
		res[host] = client
	}
	return res
}

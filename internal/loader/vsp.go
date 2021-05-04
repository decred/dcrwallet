package loader

import (
	"sync"

	"decred.org/dcrwallet/v2/errors"
	"decred.org/dcrwallet/v2/internal/vsp"
)

var vspClients = struct {
	mu      sync.Mutex
	clients map[string]*vsp.Client
}{
	clients: make(map[string]*vsp.Client),
}

// VSP loads or creates a package-global instance of the VSP client for a host.
// This allows clients to be created and reused across various subsystems.
func VSP(cfg vsp.Config) (*vsp.Client, error) {
	key := cfg.URL
	vspClients.mu.Lock()
	defer vspClients.mu.Unlock()
	client, ok := vspClients.clients[key]
	if ok {
		return client, nil
	}
	client, err := vsp.New(cfg)
	if err != nil {
		return nil, err
	}
	vspClients.clients[key] = client
	return client, nil
}

// LookupVSP returns a previously-configured VSP client, if one has been created
// and registered with the VSP function.  Otherwise, a NotExist error is
// returned.
func LookupVSP(host string) (*vsp.Client, error) {
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
func AllVSPs() map[string]*vsp.Client {
	// Create a copy to avoid callers mutating the list.
	vspClients.mu.Lock()
	defer vspClients.mu.Unlock()
	res := make(map[string]*vsp.Client, len(vspClients.clients))
	for host, client := range vspClients.clients {
		res[host] = client
	}
	return res
}

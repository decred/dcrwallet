package wallet

import (
	"decred.org/dcrwallet/v5/errors"
	"decred.org/dcrwallet/v5/internal/loggers"
)

// VSP loads or creates a package-global instance of the VSP client for a host.
// This allows clients to be created and reused across various subsystems.
func (w *Wallet) VSP(cfg VSPClientConfig) (*VSPClient, error) {
	key := cfg.URL
	w.vspClientsMu.Lock()
	defer w.vspClientsMu.Unlock()
	client, ok := w.vspClients[key]
	if ok {
		return client, nil
	}
	client, err := w.NewVSPClient(cfg, loggers.VspcLog)
	if err != nil {
		return nil, err
	}
	w.vspClients[key] = client
	return client, nil
}

// LookupVSP returns a previously-configured VSP client, if one has been created
// and registered with the VSP function.  Otherwise, a NotExist error is
// returned.
func (w *Wallet) LookupVSP(host string) (*VSPClient, error) {
	w.vspClientsMu.Lock()
	defer w.vspClientsMu.Unlock()
	client, ok := w.vspClients[host]
	if !ok {
		err := errors.Errorf("VSP client for %q not found", host)
		return nil, errors.E(errors.NotExist, err)
	}
	return client, nil
}

// AllVSPs returns the list of all currently registered VSPs.
func (w *Wallet) AllVSPs() map[string]*VSPClient {
	// Create a copy to avoid callers mutating the list.
	w.vspClientsMu.Lock()
	defer w.vspClientsMu.Unlock()
	res := make(map[string]*VSPClient, len(w.vspClients))
	for host, client := range w.vspClients {
		res[host] = client
	}
	return res
}

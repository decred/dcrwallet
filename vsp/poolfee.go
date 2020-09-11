package vsp

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io/ioutil"
	"net/http"
)

func (v *VSP) PoolFee(ctx context.Context) (float64, error) {
	url := protocol + v.hostname + apiVSPInfo

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		log.Errorf("failed to create new fee address request: %v", err)
		return -1, err
	}
	resp, err := v.httpClient.Do(req)
	if err != nil {
		log.Errorf("vspinfo request failed: %v", err)
		return -1, err
	}
	// TODO - Add numBytes resp check

	responseBody, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		log.Errorf("failed to read fee ddress response: %v", err)
		return -1, err
	}
	if resp.StatusCode != http.StatusOK {
		log.Warnf("vsp responded with an error: %v", string(responseBody))
		return -1, err
	}

	serverSigStr := resp.Header.Get(serverSignature)
	if serverSigStr == "" {
		log.Warnf("vspinfo response missing server signature")
		return -1, err
	}
	serverSig, err := base64.StdEncoding.DecodeString(serverSigStr)
	if err != nil {
		log.Warnf("failed to decode server signature: %v", err)
		return -1, err
	}

	if !ed25519.Verify(v.pubKey, responseBody, serverSig) {
		log.Warnf("server failed verification")
		return -1, err
	}

	var vspInfo vspInfoResponse
	err = json.Unmarshal(responseBody, &vspInfo)
	if err != nil {
		log.Warnf("failed to unmarshal vspinfo response: %v", err)
		return -1, err
	}

	return vspInfo.FeePercentage, nil
}

package vsp

import "context"

func (v *VSP) PoolFee(ctx context.Context) (float64, error) {
	var vspInfo vspInfoResponse
	err := v.client.get(ctx, "/api/v3/vspinfo", &vspInfo)
	if err != nil {
		return -1, err
	}
	return vspInfo.FeePercentage, nil
}

package response

import commonmodel "shopnexus-server/internal/shared/model"

type CommonResponse struct {
	Data  any                `json:"data,omitempty"`
	Error *commonmodel.Error `json:"error"`
}

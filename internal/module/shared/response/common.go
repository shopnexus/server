package response

import commonmodel "shopnexus-remastered/internal/module/common/model"

type CommonResponse struct {
	Data  any                `json:"data,omitempty"`
	Error *commonmodel.Error `json:"error"`
}

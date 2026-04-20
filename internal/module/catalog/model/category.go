package catalogmodel

import (
	commonmodel "shopnexus-server/internal/module/common/model"

	"github.com/google/uuid"
)

type Category struct {
	ID          uuid.UUID              `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	ParentID    uuid.NullUUID          `json:"parent_id"`
	Resources   []commonmodel.Resource `json:"resources"`
}

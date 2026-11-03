package handler

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	"net/url"
)

func archiveFieldQuery(values url.Values) (domain.ArchiveFieldSelector, error) {
	out := domain.ArchiveFieldSelector{Code: values.Get("field"), Status: values.Get("field_status")}
	if len(values["field"]) > 1 || len(values["field_status"]) > 1 || !out.Valid() {
		return out, app.ErrInvalid
	}
	return out, nil
}
